// Package applications implements the Checkmarx One Applications API.
package applications

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// Path is the relative path to the applications resource.
const Path = "api/applications"

// List returns a paginated list of applications. GET /api/applications → 200.
func List(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (*models.ApplicationsResponseModel, error) {
	var out models.ApplicationsResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Iter returns an item-yielding iterator over applications matching query.
// Same offset-handling pattern as projects.Iter / scans.Iter.
func Iter(e *transport.Executor, baseURL string, query url.Values, cfg pagination.Config) *pagination.Iterator[models.Application] {
	cfg.OffsetIsByCount = true
	if cfg.PageSize == 0 {
		cfg.PageSize = 100
	}
	return pagination.New(cfg, func(ctx context.Context, offset, limit int) ([]models.Application, error) {
		q := make(url.Values, len(query)+2)
		for k, vs := range query {
			cp := make([]string, len(vs))
			copy(cp, vs)
			q[k] = cp
		}
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(limit))
		coll, err := List(ctx, e, baseURL, q)
		if err != nil {
			return nil, err
		}
		return coll.Applications, nil
	})
}

// Update replaces the editable fields of an application.
// PUT /api/applications/{applicationId} → 204.
func Update(ctx context.Context, e *transport.Executor, baseURL, appID string, in *models.ApplicationConfiguration) error {
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(baseURL, Path+"/"+appID),
		nil, in, []int{http.StatusNoContent}, nil)
}

// AssociateProjects attaches one or more projects to an application.
// POST /api/applications/{applicationId}/projects → 201.
func AssociateProjects(ctx context.Context, e *transport.Executor, baseURL, appID string, projectIDs []string) error {
	body := models.AssociateProjectModel{ProjectIDs: projectIDs}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+appID+"/projects"),
		nil, body, []int{http.StatusCreated}, nil)
}

// Get fetches a single application by id. GET /api/applications/{id} → 200.
func Get(ctx context.Context, e *transport.Executor, baseURL, appID string) (*models.Application, error) {
	var out models.Application
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+appID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Create creates a new application. POST /api/applications → 201.
func Create(ctx context.Context, e *transport.Executor, baseURL string, in *models.ApplicationCreateRequest) (*models.Application, error) {
	if in == nil || in.Name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "application.Name", Reason: "is required"}
	}
	// Defaults that Checkmarx One rejects if null on some tenants.
	if in.Rules == nil {
		in.Rules = []models.ApplicationRule{}
	}
	if in.Tags == nil {
		in.Tags = map[string]string{}
	}
	var out models.Application
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, in, []int{http.StatusCreated, http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete removes an application. DELETE /api/applications/{id} → 204.
func Delete(ctx context.Context, e *transport.Executor, baseURL, appID string) error {
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path+"/"+appID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// Patch applies a partial update to an application (Checkmarx One 3.41+).
// PATCH /api/applications/{id} → 204.
func Patch(ctx context.Context, e *transport.Executor, baseURL, appID string, patch *models.ApplicationPatch) error {
	return transport.DoJSON(ctx, e, http.MethodPatch,
		transport.JoinURL(baseURL, Path+"/"+appID),
		nil, patch, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// Count returns the total number of applications matching query. The
// backing /api/applications endpoint already reports TotalCount on page
// metadata, so Count issues a limit=1 request and returns the envelope's
// count fields.
func Count(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (int, error) {
	q := make(url.Values, len(query)+1)
	for k, vs := range query {
		cp := make([]string, len(vs))
		copy(cp, vs)
		q[k] = cp
	}
	q.Set("limit", "1")
	q.Set("offset", "0")
	resp, err := List(ctx, e, baseURL, q)
	if err != nil {
		return 0, err
	}
	if resp.FilteredTotalCount > 0 {
		return resp.FilteredTotalCount, nil
	}
	return resp.TotalCount, nil
}

// AssignProjectsDirect attaches an application directly to one or more
// projects using the DIRECT_APP_ASSOCIATION payload variant. Requires the
// tenant's DIRECT_APP_ASSOCIATION_ENABLED feature flag.
// POST /api/applications/{id}/projects → 201.
func AssignProjectsDirect(ctx context.Context, e *transport.Executor, baseURL, appID string, projectIDs []string) error {
	body := models.ApplicationProjectsModel{Projects: projectIDs}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+appID+"/projects"),
		nil, body, []int{http.StatusCreated, http.StatusOK, http.StatusNoContent}, nil)
}

// RemoveProjectsDirect detaches an application from one or more projects.
// DELETE /api/applications/{id}/projects → 204. Same feature-flag requirement
// as [AssignProjectsDirect].
func RemoveProjectsDirect(ctx context.Context, e *transport.Executor, baseURL, appID string, projectIDs []string) error {
	body := models.ApplicationProjectsModel{Projects: projectIDs}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path+"/"+appID+"/projects"),
		nil, body, []int{http.StatusNoContent, http.StatusOK}, nil)
}

