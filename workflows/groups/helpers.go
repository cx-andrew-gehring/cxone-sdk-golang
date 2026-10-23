package groups

import (
	"context"
	"fmt"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/groups"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// GetOrCreateByName returns the existing top-level group whose name matches,
// or creates a new one if none exists.
func GetOrCreateByName(ctx context.Context, backend *Backend, name string) (*models.Group, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "name", Reason: "is required"}
	}

	existing, err := findByName(ctx, backend, name)
	if err != nil {
		return nil, fmt.Errorf("lookup group %q: %w", name, err)
	}
	if existing != nil {
		return existing, nil
	}

	return endpoints.CreateAdmin(ctx, backend.Executor, backend.IAMAdminURL, &models.Group{Name: name})
}

func findByName(ctx context.Context, b *Backend, name string) (*models.Group, error) {
	groups, err := endpoints.ListAdmin(ctx, b.Executor, b.IAMAdminURL, models.GroupFilter{
		Search: name,
		Max:    20,
	})
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i], nil
		}
	}
	return nil, nil
}
