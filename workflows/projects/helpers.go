package projects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

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
		if !isClientConflict(err) {
			return nil, err
		}
		// Lost a create race (another caller created the same name between
		// our lookup and create). Re-run the exact-name lookup and return the
		// winner. A confirmed duplicate (HTTP 400 "... already exists",
		// measured on the canary) is retried a few times with a short
		// backoff, because the winner may not be visible to the listing yet;
		// any other 4xx gets a single re-lookup.
		dup := isAlreadyExists(err)
		tries := 1
		if dup {
			tries = len(conflictLookupBackoff)
		}
		for i := 0; i < tries; i++ {
			if dup {
				if serr := sleepCtx(ctx, conflictLookupBackoff[i]); serr != nil {
					return nil, serr
				}
			}
			found, lerr := findProjectByName(ctx, backend, template.Name)
			if lerr == nil && found != nil {
				return &ProjectRepoConfig{backend: backend, project: found}, nil
			}
			if cerr := ctx.Err(); cerr != nil {
				return nil, cerr
			}
		}
		if dup {
			return nil, &cxerrors.ConflictError{Resource: "project", Name: template.Name, Cause: err}
		}
		return nil, err
	}
	return &ProjectRepoConfig{backend: backend, project: created}, nil
}

// conflictLookupBackoff is the delay before each exact-name re-lookup after a
// duplicate-name create (3 tries, 3.5 s worst case in total).
var conflictLookupBackoff = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}

// isAlreadyExists reports whether err is the service's duplicate-name
// rejection. Measured (spike S5): HTTP 400, body code 208, message
// "Failed to create a project, project name '<name>' already exists". The
// message text is the only discriminator; 409 is accepted too.
func isAlreadyExists(err error) bool {
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) {
		return false
	}
	if re.StatusCode != http.StatusBadRequest && re.StatusCode != http.StatusConflict {
		return false
	}
	return strings.Contains(strings.ToLower(re.Reason), "already exists")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
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
