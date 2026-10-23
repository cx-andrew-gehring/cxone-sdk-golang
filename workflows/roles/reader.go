package roles

import (
	"context"
	"sync"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/roles"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// RoleReader is a two-level cached view of the tenant's role catalog.
//
//   - Level 1: flat list of realm roles + client roles, indexed by ID and
//     name. Lazy-loaded on first access.
//   - Level 2: per-role composite resolution via [RoleWithComposites].
//     Lazy-loaded per role on first call to Composites().
//
// Thread-safe. Construct with [NewReader] and reuse for the client lifetime.
type RoleReader struct {
	backend *Backend

	mu      sync.Mutex
	loaded  bool
	all     []*roleEntry
	byID    map[string]*roleEntry
	byName  map[string]*roleEntry
}

type roleEntry struct {
	models.Role
	mu             sync.Mutex
	compositesErr  error
	composites     []models.Role
	compositesLoad bool
}

// NewReader constructs an empty RoleReader.
func NewReader(backend *Backend) *RoleReader {
	if backend == nil {
		return &RoleReader{}
	}
	return &RoleReader{backend: backend}
}

func (r *RoleReader) ensureLoaded(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return nil
	}
	if r.backend == nil || r.backend.Executor == nil {
		return &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	realmRoles, err := endpoints.ListRealm(ctx, r.backend.Executor, r.backend.IAMAdminURL)
	if err != nil {
		return err
	}
	r.byID = make(map[string]*roleEntry, len(realmRoles)*2)
	r.byName = make(map[string]*roleEntry, len(realmRoles)*2)
	r.all = make([]*roleEntry, 0, len(realmRoles))
	for _, role := range realmRoles {
		e := &roleEntry{Role: role}
		r.all = append(r.all, e)
		r.byID[e.ID] = e
		r.byName[e.Name] = e
	}
	r.loaded = true
	return nil
}

// List returns all realm roles. Triggers a one-time fetch on first call.
func (r *RoleReader) List(ctx context.Context) ([]models.Role, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.Role, len(r.all))
	for i, e := range r.all {
		out[i] = e.Role
	}
	return out, nil
}

// ByID returns the role with the given ID, or nil.
func (r *RoleReader) ByID(ctx context.Context, id string) (*models.Role, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	e := r.byID[id]
	r.mu.Unlock()
	if e == nil {
		return nil, nil
	}
	role := e.Role
	return &role, nil
}

// ByName returns the role with the given name, or nil.
func (r *RoleReader) ByName(ctx context.Context, name string) (*models.Role, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	e := r.byName[name]
	r.mu.Unlock()
	if e == nil {
		return nil, nil
	}
	role := e.Role
	return &role, nil
}

// Composites returns the sub-roles of a composite role, lazily fetching
// and caching them on first access. Returns nil for non-composite roles.
func (r *RoleReader) Composites(ctx context.Context, roleID string) ([]models.Role, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	e := r.byID[roleID]
	r.mu.Unlock()
	if e == nil {
		return nil, nil
	}
	if !e.Composite {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.compositesLoad {
		return e.composites, e.compositesErr
	}
	subs, err := endpoints.GetComposites(ctx, r.backend.Executor, r.backend.IAMAdminURL, roleID)
	e.composites = subs
	e.compositesErr = err
	e.compositesLoad = true
	return subs, err
}

// ListClientRoles returns all roles under a specific Keycloak client. This
// is NOT cached — it issues a fresh API call each time because client role
// catalogs are typically small and change infrequently.
func (r *RoleReader) ListClientRoles(ctx context.Context, clientID string) ([]models.Role, error) {
	if r.backend == nil || r.backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	return endpoints.ListClient(ctx, r.backend.Executor, r.backend.IAMAdminURL, clientID)
}
