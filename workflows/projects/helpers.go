package projects

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	endpoints "github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/projects"
	appEndpoints "github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/applications"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
)

// GetOrCreateByName returns the existing project whose name matches, or
// creates a new project with the supplied template if none exists. The
// returned [ProjectRepoConfig] is always backed by a server-side record.
func GetOrCreateByName(ctx context.Context, backend *Backend, template *models.Project) (*ProjectRepoConfig, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if template == nil || template.Name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "project.Name", Reason: "is required"}
	}

	existing, err := findProjectByName(ctx, backend, template.Name)
	if err != nil {
		return nil, fmt.Errorf("lookup project %q: %w", template.Name, err)
	}
	if existing != nil {
		return &ProjectRepoConfig{backend: backend, project: existing}, nil
	}

	created, err := endpoints.Create(ctx, backend.Executor, backend.BaseURL, template)
	if err != nil {
		// Lost a create race (another caller created the same name between
		// our lookup and create): on a 4xx conflict/duplicate, re-run the
		// exact-name lookup once and return the winner.
		if isClientConflict(err) {
			if found, lerr := findProjectByName(ctx, backend, template.Name); lerr == nil && found != nil {
				return &ProjectRepoConfig{backend: backend, project: found}, nil
			}
		}
		return nil, err
	}
	return &ProjectRepoConfig{backend: backend, project: created}, nil
}

// isClientConflict reports whether err is an HTTP 4xx rejection (the shape of
// a duplicate-name conflict: 400/409) of the create call.
func isClientConflict(err error) bool {
	var re *cxerrors.ResponseError
	if errors.As(err, &re) {
		return re.StatusCode >= 400 && re.StatusCode < 500 && re.StatusCode != 401 && re.StatusCode != 403 && re.StatusCode != 429
	}
	var ce *cxerrors.CommunicationError
	if errors.As(err, &ce) {
		return ce.StatusCode >= 400 && ce.StatusCode < 500 && ce.StatusCode != 401 && ce.StatusCode != 403 && ce.StatusCode != 429
	}
	return false
}

// GetOrCreateInApplicationByName finds or creates a project with the given
// name, then ensures it is associated with the named application (creating
// the application too if necessary). Returns both the project config and the
// application model.
func GetOrCreateInApplicationByName(ctx context.Context, backend *Backend, projectName, applicationName string) (*ProjectRepoConfig, *models.Application, error) {
	if backend == nil || backend.Executor == nil {
		return nil, nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if projectName == "" || applicationName == "" {
		return nil, nil, &cxerrors.ConfigurationError{Field: "projectName/applicationName", Reason: "both are required"}
	}

	// Find or create the application.
	app, err := findApplicationByName(ctx, backend, applicationName)
	if err != nil {
		return nil, nil, fmt.Errorf("lookup application %q: %w", applicationName, err)
	}
	if app == nil {
		app, err = appEndpoints.Create(ctx, backend.Executor, backend.BaseURL, &models.ApplicationCreateRequest{
			Name: applicationName,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("create application %q: %w", applicationName, err)
		}
	}

	// Find or create the project.
	repo, err := GetOrCreateByName(ctx, backend, &models.Project{
		Name:           projectName,
		ApplicationIDs: []string{app.ID},
	})
	if err != nil {
		return nil, nil, err
	}

	return repo, app, nil
}

func findProjectByName(ctx context.Context, b *Backend, name string) (*models.ProjectResponseModel, error) {
	// names= is an exact-match filter (projects.yaml). The previous name=
	// lookup was a substring match capped at limit=10, which could miss the
	// exact project (when >10 names contain it) and cause a duplicate create.
	q := url.Values{"names": {name}, "limit": {"10"}}
	coll, err := endpoints.List(ctx, b.Executor, b.BaseURL, q)
	if err != nil {
		return nil, err
	}
	for i := range coll.Projects {
		if coll.Projects[i].Name == name {
			return &coll.Projects[i], nil
		}
	}
	return nil, nil
}

func findApplicationByName(ctx context.Context, b *Backend, name string) (*models.Application, error) {
	q := url.Values{"name": {name}, "limit": {"10"}}
	coll, err := appEndpoints.List(ctx, b.Executor, b.BaseURL, q)
	if err != nil {
		return nil, err
	}
	for i := range coll.Applications {
		if coll.Applications[i].Name == name {
			return &coll.Applications[i], nil
		}
	}
	return nil, nil
}
