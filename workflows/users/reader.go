package users

import (
	"context"
	"sync"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/users"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// UserReader is a cached view of the tenant's user catalog. It lazy-loads
// the full user list on first access and builds lookup indices by ID, email,
// and username. Subsequent lookups return from the in-memory cache.
//
// Thread-safe. Construct with [NewReader] and reuse for the client lifetime.
// The cache is NOT auto-invalidated — construct a new reader to observe
// server-side changes.
type UserReader struct {
	backend *Backend

	mu         sync.Mutex
	loaded     bool
	users      []models.User
	byID       map[string]*models.User
	byEmail    map[string]*models.User
	byUsername map[string]*models.User
}

// NewReader constructs an empty UserReader.
func NewReader(backend *Backend) *UserReader {
	if backend == nil {
		return &UserReader{}
	}
	return &UserReader{backend: backend}
}

func (r *UserReader) ensureLoaded(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return nil
	}
	if r.backend == nil || r.backend.Executor == nil {
		return &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	all, err := fetchAllUsers(ctx, r.backend)
	if err != nil {
		return err
	}
	r.users = all
	r.byID = make(map[string]*models.User, len(all))
	r.byEmail = make(map[string]*models.User, len(all))
	r.byUsername = make(map[string]*models.User, len(all))
	for i := range r.users {
		u := &r.users[i]
		r.byID[u.ID] = u
		if u.Email != "" {
			r.byEmail[u.Email] = u
		}
		if u.Username != "" {
			r.byUsername[u.Username] = u
		}
	}
	r.loaded = true
	return nil
}

// List returns all users. Triggers a one-time full fetch on first call.
func (r *UserReader) List(ctx context.Context) ([]models.User, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.User, len(r.users))
	copy(out, r.users)
	return out, nil
}

// ByID returns the user with the given ID, or nil.
func (r *UserReader) ByID(ctx context.Context, id string) (*models.User, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id], nil
}

// ByEmail returns the user with the given email, or nil.
func (r *UserReader) ByEmail(ctx context.Context, email string) (*models.User, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byEmail[email], nil
}

// ByUsername returns the user with the given username, or nil.
func (r *UserReader) ByUsername(ctx context.Context, username string) (*models.User, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byUsername[username], nil
}

// Count returns the total number of users on the tenant without fetching
// the full list. This is a direct API call, not cached.
func (r *UserReader) Count(ctx context.Context) (uint64, error) {
	if r.backend == nil || r.backend.Executor == nil {
		return 0, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	return endpoints.Count(ctx, r.backend.Executor, r.backend.IAMAdminURL, models.UserFilter{})
}

// fetchAllUsers pages through the user list endpoint to retrieve every user.
func fetchAllUsers(ctx context.Context, b *Backend) ([]models.User, error) {
	const pageSize = 100
	var all []models.User
	for offset := 0; ; offset += pageSize {
		page, err := endpoints.List(ctx, b.Executor, b.IAMAdminURL, models.UserFilter{
			First: offset,
			Max:   pageSize,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
	}
	return all, nil
}
