// Package presets implements the high-level preset / query catalog reader.
//
// CLAUDE.md §9.5 specifies a multi-level lazy cache:
//
//   - Level 1: list of presets, indexed by name.
//   - Level 2: query families per engine (one fetch per family).
//   - Level 3: queries per family, indexed by id.
//
// Each level has its own [sync.Mutex] and lazy-initializes on first access.
// Independent siblings are fanned out concurrently with [errgroup.Group].
//
// In Go we don't have C#-style nested classes, so each level is its own
// type. The top-level [PresetReader] hangs onto a level-1 cache; it returns
// [Preset] handles whose own caches make up level 2; those return
// [QueryFamily] handles whose caches make up level 3.
//
// Important note (TODO(spec)): the underlying preset endpoint URLs are not
// in ast-cli (the CLI passes preset names through as opaque strings). The
// schema-shape assumptions in [models.PresetCollection] / [models.QueryCollection]
// are best-effort and need verification against the live swagger before
// shipping a stable release. The CACHING contract here, however, is the
// part CLAUDE.md actually mandates — that part is implementation-correct
// regardless of how the underlying endpoint shape evolves.
package presets

import (
	"context"
	"sort"
	"sync"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/presets"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Backend bundles the dependencies the workflow needs to call the
// underlying endpoint package.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}

// PresetReader is the multi-level cached view of the platform's SAST preset
// + query catalog.
//
// All methods are safe for concurrent use. The first access to any cache
// level triggers a single fetch shared by all concurrent callers; subsequent
// accesses return from the in-memory cache.
//
// Construct one with [NewReader] and reuse it for the lifetime of the
// containing cxone client. The reader's caches are NOT invalidated
// automatically — to observe a server-side change to the preset catalog,
// construct a new reader.
type PresetReader struct {
	backend *Backend

	// Level 1: list-of-presets cache. Lazy-loaded on first call to any
	// method that needs it (List, ByName, ByID).
	mu       sync.Mutex
	loaded   bool
	loadErr  error
	byID     map[string]*Preset
	byName   map[string]*Preset
	ordered  []*Preset
}

// NewReader constructs an empty [PresetReader]. The first call to any of
// its methods triggers the level-1 fetch.
func NewReader(backend *Backend) *PresetReader {
	if backend == nil {
		return &PresetReader{}
	}
	return &PresetReader{backend: backend}
}

// ensureLoaded performs the level-1 fetch under c.mu. Subsequent callers
// see the cached state. On error the cache is reset so the next caller can
// retry; this matches the ProjectRepoConfig pattern from §9.3.
func (r *PresetReader) ensureLoaded(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return nil
	}
	if r.backend == nil || r.backend.Executor == nil {
		return &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	coll, err := endpoints.ListPresets(ctx, r.backend.Executor, r.backend.BaseURL, nil)
	if err != nil {
		r.loadErr = err
		return err
	}
	r.byID = make(map[string]*Preset, len(coll.Presets))
	r.byName = make(map[string]*Preset, len(coll.Presets))
	r.ordered = make([]*Preset, 0, len(coll.Presets))
	for i := range coll.Presets {
		desc := coll.Presets[i]
		p := newPreset(r.backend, desc)
		r.byID[desc.ID] = p
		r.byName[desc.Name] = p
		r.ordered = append(r.ordered, p)
	}
	sort.Slice(r.ordered, func(i, j int) bool { return r.ordered[i].Name() < r.ordered[j].Name() })
	r.loaded = true
	return nil
}

// List returns all presets in name-sorted order. Triggers a one-time
// level-1 fetch on first call.
func (r *PresetReader) List(ctx context.Context) ([]*Preset, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Preset, len(r.ordered))
	copy(out, r.ordered)
	return out, nil
}

// ByName returns the preset with the given name, or nil if not found.
func (r *PresetReader) ByName(ctx context.Context, name string) (*Preset, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byName[name], nil
}

// ByID returns the preset with the given UUID, or nil if not found.
func (r *PresetReader) ByID(ctx context.Context, id string) (*Preset, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id], nil
}

// LoadAllDetails fans out level-2 detail fetches for every preset in
// parallel using a bounded worker pool, populating each preset's cached
// detail. Useful for pre-warming the cache before a hot loop.
//
// Returns the first error encountered if any individual fetch fails; the
// successful fetches still populate their respective caches.
func (r *PresetReader) LoadAllDetails(ctx context.Context) error {
	all, err := r.List(ctx)
	if err != nil {
		return err
	}
	// Bound concurrency so we don't hammer the platform with N parallel
	// requests for a large catalog. Implemented as a buffered channel
	// semaphore — no third-party dependency needed.
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)

	var (
		wg      sync.WaitGroup
		firstMu sync.Mutex
		first   error
	)
	for _, p := range all {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := p.Detail(ctx); err != nil {
				firstMu.Lock()
				if first == nil {
					first = err
				}
				firstMu.Unlock()
			}
		}()
	}
	wg.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}

// Preset is a level-2 cached handle for one preset. It holds the lightweight
// descriptor (loaded with the level-1 fetch) and lazily loads the full
// detail on first access.
type Preset struct {
	backend *Backend
	desc    models.PresetDescriptor

	mu      sync.Mutex
	loaded  bool
	loadErr error
	detail  *models.PresetDetail
}

func newPreset(backend *Backend, desc models.PresetDescriptor) *Preset {
	return &Preset{backend: backend, desc: desc}
}

// ID returns the preset's UUID.
func (p *Preset) ID() string { return p.desc.ID }

// Name returns the preset name.
func (p *Preset) Name() string { return p.desc.Name }

// Description returns the human-readable description, if any.
func (p *Preset) Description() string { return p.desc.Description }

// Custom reports whether this is a tenant-customized preset (vs a built-in).
func (p *Preset) Custom() bool { return p.desc.Custom }

// Detail returns the full preset detail (including its query id list),
// fetching it on the first call and caching the result.
//
// Concurrent callers serialize on a single fetch — only the first goroutine
// sends a network request; subsequent callers wait and reuse the cache.
func (p *Preset) Detail(ctx context.Context) (*models.PresetDetail, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		if p.loadErr != nil {
			err := p.loadErr
			// Reset for retry on next call.
			p.loaded = false
			p.loadErr = nil
			return nil, err
		}
		return p.detail, nil
	}
	d, err := endpoints.GetPreset(ctx, p.backend.Executor, p.backend.BaseURL, p.desc.ID)
	p.loaded = true
	if err != nil {
		p.loadErr = err
		// Reset for retry on next call.
		p.loaded = false
		return nil, err
	}
	p.detail = d
	return d, nil
}
