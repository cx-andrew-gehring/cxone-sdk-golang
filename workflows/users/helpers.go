package users

import (
	"context"
	"fmt"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/users"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// GetOrCreateByUsername returns the existing user whose username matches, or
// creates a new user with the supplied template if none exists. The returned
// user is always the server-side version (with ID populated).
func GetOrCreateByUsername(ctx context.Context, backend *Backend, template *models.User) (*models.User, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if template == nil || template.Username == "" {
		return nil, &cxerrors.ConfigurationError{Field: "user.Username", Reason: "is required"}
	}
	existing, err := findByUsername(ctx, backend, template.Username)
	if err != nil {
		return nil, fmt.Errorf("lookup user %q: %w", template.Username, err)
	}
	if existing != nil {
		return existing, nil
	}
	return endpoints.Create(ctx, backend.Executor, backend.IAMAdminURL, template)
}

// GetOrCreateByEmail returns the existing user whose email matches, or
// creates a new user with the supplied template if none exists.
func GetOrCreateByEmail(ctx context.Context, backend *Backend, template *models.User) (*models.User, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if template == nil || template.Email == "" {
		return nil, &cxerrors.ConfigurationError{Field: "user.Email", Reason: "is required"}
	}
	existing, err := findByEmail(ctx, backend, template.Email)
	if err != nil {
		return nil, fmt.Errorf("lookup user %q: %w", template.Email, err)
	}
	if existing != nil {
		return existing, nil
	}
	return endpoints.Create(ctx, backend.Executor, backend.IAMAdminURL, template)
}

func findByUsername(ctx context.Context, b *Backend, username string) (*models.User, error) {
	falsy := false
	users, err := endpoints.List(ctx, b.Executor, b.IAMAdminURL, models.UserFilter{
		Max:      10,
		Username: username,
		Exact:    &falsy,
	})
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Username == username {
			return &users[i], nil
		}
	}
	return nil, nil
}

func findByEmail(ctx context.Context, b *Backend, email string) (*models.User, error) {
	falsy := false
	users, err := endpoints.List(ctx, b.Executor, b.IAMAdminURL, models.UserFilter{
		Max:   10,
		Email: email,
		Exact: &falsy,
	})
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Email == email {
			return &users[i], nil
		}
	}
	return nil, nil
}
