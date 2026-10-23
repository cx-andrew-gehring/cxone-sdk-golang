// Package projects implements the high-level project workflow front door.
//
// The single domain object exposed by this package is [ProjectRepoConfig] —
// a lazily-loaded view of one Checkmarx One project that combines the
// project metadata (name, branches, tags, application bindings) with its
// per-project configuration overrides (preset name, scan filters, language
// mode, …) into one ergonomic handle.
//
// CLAUDE.md §9.1 mandates that domain objects whose construction needs a
// server fetch must use an async factory rather than I/O in a constructor.
// CLAUDE.md §9.3 mandates lock-protected lazy initialization to guarantee
// a single fetch even when multiple callers await concurrently. This
// package implements both: [FromProjectID] is the async factory; the
// per-field [sync.Once] guards inside [ProjectRepoConfig] handle
// concurrent access to the cached project + configuration data.
package projects

import (
	"context"
	"sync"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/projects"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Backend bundles the dependencies a high-level workflow needs to issue
// API calls. The cxone client builds one of these and hands it to every
// workflow handle. Defined here (rather than re-importing transport.Executor
// at every call site) so the workflow packages have a single, narrow
// dependency on the transport machinery.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}

// ProjectRepoConfig is the high-level handle for one Checkmarx One project.
//
// Construction is via [FromProjectID]; calling that factory issues exactly
// one GET /api/projects/{id} and seeds the handle. Per-project configuration
// overrides are loaded lazily on the first call to [ProjectRepoConfig.Configuration]
// and cached for subsequent calls.
//
// ProjectRepoConfig is safe for concurrent use. The internal locks
// guarantee that two goroutines awaiting the same lazy field share a
// single network fetch (CLAUDE.md §9.3).
//
// Lifecycle: ProjectRepoConfig holds a reference to the parent client's
// transport executor; do NOT use a ProjectRepoConfig after the parent
// client is closed.
type ProjectRepoConfig struct {
	backend *Backend

	// Project is the snapshot fetched at construction. Read-only — to
	// observe a server-side change, construct a new ProjectRepoConfig.
	project *models.ProjectResponseModel

	// configOnce serializes the lazy fetch of the per-project configuration.
	configOnce sync.Once
	configErr  error
	config     []models.ProjectConfiguration

	// branchesOnce serializes the lazy fetch of branches. Branches change
	// over time, so callers who need a fresh list should construct a new
	// handle rather than relying on the cache. The cache is here mainly so
	// repeated checks within one workflow are cheap.
	branchesOnce sync.Once
	branchesErr  error
	branches     []string
}

// FromProjectID is the async factory for [ProjectRepoConfig].
//
// It issues GET /api/projects/{id} and returns a populated handle on
// success. The most common error is *cxerrors.ResponseError with status
// 404 when the project does not exist; check via errors.As.
//
// Side effects: exactly one HTTP request, no caching of the project
// catalog. If you need many handles in bulk, prefer
// [github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/projects.Iter]
// followed by [FromProjectModel].
func FromProjectID(ctx context.Context, backend *Backend, projectID string) (*ProjectRepoConfig, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if projectID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	proj, err := endpoints.Get(ctx, backend.Executor, backend.BaseURL, projectID)
	if err != nil {
		return nil, err
	}
	return &ProjectRepoConfig{backend: backend, project: proj}, nil
}

// FromProjectModel constructs a [ProjectRepoConfig] from an already-fetched
// project (e.g. from an iterator walk). No additional HTTP request is
// issued; the configuration and branches still load lazily on first access.
//
// Use this when you already have the [models.ProjectResponseModel] in hand
// and want to skip the redundant per-handle GET.
func FromProjectModel(backend *Backend, project *models.ProjectResponseModel) (*ProjectRepoConfig, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if project == nil {
		return nil, &cxerrors.ConfigurationError{Field: "project", Reason: "is required"}
	}
	return &ProjectRepoConfig{backend: backend, project: project}, nil
}

// Project returns the cached project metadata snapshot. The returned
// pointer is shared with the handle — do not mutate it.
func (p *ProjectRepoConfig) Project() *models.ProjectResponseModel { return p.project }

// ID returns the project's UUID.
func (p *ProjectRepoConfig) ID() string { return p.project.ID }

// Name returns the project's name.
func (p *ProjectRepoConfig) Name() string { return p.project.Name }

// RepoURL returns the project's primary repository URL, or "" if none is set.
func (p *ProjectRepoConfig) RepoURL() string { return p.project.RepoURL }

// MainBranch returns the project's primary branch name, or "" if not set.
func (p *ProjectRepoConfig) MainBranch() string { return p.project.MainBranch }

// Tags returns the project's tags. The returned map is shared; clone it
// before mutating.
func (p *ProjectRepoConfig) Tags() map[string]string { return p.project.Tags }

// Groups returns the project's IAM group IDs.
func (p *ProjectRepoConfig) Groups() []string { return p.project.Groups }

// ApplicationIDs returns the application UUIDs the project is bound to.
func (p *ProjectRepoConfig) ApplicationIDs() []string { return p.project.ApplicationIDs }

// Configuration returns the per-project configuration overrides, fetching
// them on the first call and caching the result.
//
// Concurrent callers serialize on a single fetch (CLAUDE.md §9.3) — only
// the first goroutine sends a network request; subsequent callers wait
// and reuse the cached result.
func (p *ProjectRepoConfig) Configuration(ctx context.Context) ([]models.ProjectConfiguration, error) {
	p.configOnce.Do(func() {
		p.config, p.configErr = endpoints.GetConfiguration(ctx, p.backend.Executor, p.backend.BaseURL, p.project.ID)
	})
	if p.configErr != nil {
		// Capture before reset — resetConfigOnce nils the field so a
		// subsequent caller can retry the fetch.
		err := p.configErr
		p.resetConfigOnce()
		return nil, err
	}
	return p.config, nil
}

// resetConfigOnce replaces configOnce with a fresh sync.Once so the next
// Configuration call retries. Called when the cached fetch errored — we
// don't want a single transient 5xx to poison the handle for its lifetime.
//
// This is racy in the strict-Go-memory-model sense (two concurrent callers
// after a failure could both observe the new Once and double-fetch), but
// the worst case is a doubled retry, which is acceptable.
func (p *ProjectRepoConfig) resetConfigOnce() {
	p.configOnce = sync.Once{}
	p.configErr = nil
	p.config = nil
}

// Branches returns the project's branches, fetching them on first call and
// caching the result. Same lazy-init semantics as [Configuration].
func (p *ProjectRepoConfig) Branches(ctx context.Context) ([]string, error) {
	p.branchesOnce.Do(func() {
		p.branches, p.branchesErr = endpoints.GetBranches(ctx, p.backend.Executor, p.backend.BaseURL, p.project.ID, nil)
	})
	if p.branchesErr != nil {
		err := p.branchesErr
		p.resetBranchesOnce()
		return nil, err
	}
	return p.branches, nil
}

func (p *ProjectRepoConfig) resetBranchesOnce() {
	p.branchesOnce = sync.Once{}
	p.branchesErr = nil
	p.branches = nil
}
