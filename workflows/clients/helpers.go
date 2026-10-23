package clients

import (
	"context"
	"fmt"
	"sync"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/clients"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// GetOrCreateByName returns the OIDC client whose clientId (display name)
// matches name, or creates a new service-account client with that name and
// the supplied defaults. The returned client always has an ID populated.
func GetOrCreateByName(ctx context.Context, backend *Backend, name string, defaults *models.OIDCClient) (*models.OIDCClient, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "name", Reason: "is required"}
	}
	existing, err := findByName(ctx, backend, name)
	if err != nil {
		return nil, fmt.Errorf("lookup client %q: %w", name, err)
	}
	if existing != nil {
		return existing, nil
	}
	if defaults == nil {
		defaults = &models.OIDCClient{}
	}
	toCreate := *defaults
	toCreate.ClientID = name
	if toCreate.Protocol == "" {
		toCreate.Protocol = "openid-connect"
	}
	toCreate.ServiceAccountsEnabled = true
	toCreate.PublicClient = false
	if err := endpoints.Create(ctx, backend.Executor, backend.IAMAdminURL, &toCreate); err != nil {
		return nil, err
	}
	return findByName(ctx, backend, name)
}

func findByName(ctx context.Context, b *Backend, name string) (*models.OIDCClient, error) {
	falsy := false
	list, err := endpoints.List(ctx, b.Executor, b.IAMAdminURL, models.OIDCClientFilter{
		Max:      10,
		ClientID: name,
		Search:   &falsy,
	})
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ClientID == name {
			return &list[i], nil
		}
	}
	return nil, nil
}

// ASTAppIDCache lazily resolves and caches the UUID of the "ast-app"
// Keycloak client. This is a per-tenant constant used by every app-role
// operation. Thread-safe; reuse one instance per backend.
type ASTAppIDCache struct {
	backend *Backend
	mu      sync.Mutex
	id      string
	err     error
	loaded  bool
}

// NewASTAppIDCache constructs a cache. The actual fetch is deferred to the
// first call to [ASTAppIDCache.Get].
func NewASTAppIDCache(backend *Backend) *ASTAppIDCache {
	return &ASTAppIDCache{backend: backend}
}

// Get returns the ast-app client UUID, fetching it at most once.
func (c *ASTAppIDCache) Get(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.id, c.err
	}
	if c.backend == nil || c.backend.Executor == nil {
		c.err = &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
		return "", c.err
	}
	client, err := findByName(ctx, c.backend, "ast-app")
	if err != nil {
		c.err = err
		return "", err
	}
	if client == nil {
		c.err = fmt.Errorf("ast-app client not found on this tenant")
		return "", c.err
	}
	c.id = client.ID
	c.loaded = true
	return c.id, nil
}
