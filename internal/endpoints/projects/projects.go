// Package projects implements the Checkmarx One Projects API.
//
// This package is part of the SDK's low-level endpoint layer (CLAUDE.md §8).
// It mirrors the Checkmarx One REST API surface 1:1 with one exported
// function per endpoint operation; each function takes (ctx, executor,
// baseURL, typed-request) and returns (typed-response, error). Endpoint
// functions never construct their own HTTP client — every call goes through
// the supplied [*transport.Executor] so the §4.3 funnel applies.
//
// Most users should NOT call this package directly. The high-level workflow
// layer (cxone/workflows/projects) wraps these primitives with fluent
// builders and async factories. Power users may reach this package through
// the cxone.Client.Advanced() handle.
package projects

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

// Path is the relative path appended to the API base URL for the projects
// resource ("https://<host>/api/" + Path → projects collection root).
const Path = "api/projects"

// Create creates a new project. POST /api/projects → 201 Created.
func Create(ctx context.Context, e *transport.Executor, baseURL string, in *models.Project) (*models.ProjectResponseModel, error) {
	var out models.ProjectResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, in, []int{http.StatusCreated}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns a paginated list of projects. GET /api/projects → 200.
//
// query is passed through verbatim — supported keys include "limit",
// "offset", "name", "ids", "tags-keys", "tags-values", "groups",
// "repo-url", and friends. The high-level workflow layer hides this with
// typed filters.
func List(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (*models.ProjectsCollectionResponseModel, error) {
	var out models.ProjectsCollectionResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Iter returns an item-yielding iterator over the projects matching query.
// The iterator transparently fetches successive pages by setting "limit"
// and "offset" on the query; any other query parameters supplied by the
// caller (filters, sort, tags) are preserved across pages.
//
// The page size defaults to 100; pass a different cfg.PageSize to override.
// cfg.OffsetIsByCount is forced to true because the projects endpoint uses
// offset-by-count semantics.
func Iter(e *transport.Executor, baseURL string, query url.Values, cfg pagination.Config) *pagination.Iterator[models.ProjectResponseModel] {
	cfg.OffsetIsByCount = true
	if cfg.PageSize == 0 {
		cfg.PageSize = 100
	}
	return pagination.New(cfg, func(ctx context.Context, offset, limit int) ([]models.ProjectResponseModel, error) {
		q := cloneValues(query)
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(limit))
		coll, err := List(ctx, e, baseURL, q)
		if err != nil {
			return nil, err
		}
		return coll.Projects, nil
	})
}

// cloneValues returns a shallow copy of v so the iterator can mutate the
// query string without affecting the caller's slice headers.
func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v)+2)
	for k, vs := range v {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

// Get fetches a single project by id. GET /api/projects/{id} → 200.
func Get(ctx context.Context, e *transport.Executor, baseURL, projectID string) (*models.ProjectResponseModel, error) {
	var out models.ProjectResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+projectID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update replaces the editable fields of a project. PUT /api/projects/{id} → 204.
func Update(ctx context.Context, e *transport.Executor, baseURL, projectID string, in *models.Project) error {
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(baseURL, Path+"/"+projectID),
		nil, in, []int{http.StatusNoContent}, nil)
}

// Delete removes a project by id. DELETE /api/projects/{id} → 204.
func Delete(ctx context.Context, e *transport.Executor, baseURL, projectID string) error {
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path+"/"+projectID),
		nil, nil, []int{http.StatusNoContent}, nil)
}

// GetBranches lists branches known for a project.
// GET /api/projects/branches?project-id={id}&limit=...&offset=... → 200.
//
// The Checkmarx One API uses kebab-case for the project-id query parameter;
// callers can either pass it through query or rely on the projectID arg
// (which is always set as project-id by this function).
func GetBranches(ctx context.Context, e *transport.Executor, baseURL, projectID string, query url.Values) (models.BranchList, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("project-id", projectID)
	var out models.BranchList
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/branches"),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Tags returns the map of tag keys to known values across all projects.
// GET /api/projects/tags → 200.
func Tags(ctx context.Context, e *transport.Executor, baseURL string) (map[string][]string, error) {
	out := map[string][]string{}
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/tags"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateConfiguration patches per-project configuration.
// PATCH /api/configuration/project?project-id={id} → 204.
func UpdateConfiguration(ctx context.Context, e *transport.Executor, baseURL, projectID string, cfg []models.ProjectConfiguration) error {
	q := url.Values{"project-id": {projectID}}
	return transport.DoJSON(ctx, e, http.MethodPatch,
		transport.JoinURL(baseURL, "api/configuration/project"),
		q, cfg, []int{http.StatusNoContent}, nil)
}

// GetConfiguration returns the per-project configuration overrides.
// GET /api/configuration/project?project-id={id} → 200.
func GetConfiguration(ctx context.Context, e *transport.Executor, baseURL, projectID string) ([]models.ProjectConfiguration, error) {
	q := url.Values{"project-id": {projectID}}
	var out []models.ProjectConfiguration
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, "api/configuration/project"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Count returns the total number of projects matching query by issuing a
// limit=1 list request and reading the TotalCount/FilteredTotalCount
// envelope fields.
func Count(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (int, error) {
	q := make(url.Values, len(query)+2)
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
		return int(resp.FilteredTotalCount), nil
	}
	return int(resp.TotalCount), nil
}

// Patch applies a partial update to a project (Checkmarx One 3.41+).
// PATCH /api/projects/{id} → 204.
func Patch(ctx context.Context, e *transport.Executor, baseURL, projectID string, patch *models.ProjectPatch) error {
	return transport.DoJSON(ctx, e, http.MethodPatch,
		transport.JoinURL(baseURL, Path+"/"+projectID),
		nil, patch, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// SetConfigurationKey is a convenience that patches a single project
// configuration entry — commonly used to pin a branch, preset, language
// mode, file filter, or repository.
//
// Examples of stable keys:
//
//	"scan.handler.git.branch"        // default git branch
//	"scan.handler.git.repository"    // default repository url
//	"scan.config.sast.presetName"    // SAST preset by name
//	"scan.config.sast.languageMode"  // "multi" or per-language
//	"scan.config.sast.filter"        // SAST file filter
func SetConfigurationKey(ctx context.Context, e *transport.Executor, baseURL, projectID, key, value string, allowOverride bool) error {
	setting := models.ProjectConfiguration{
		Key:           key,
		Value:         value,
		AllowOverride: allowOverride,
	}
	return UpdateConfiguration(ctx, e, baseURL, projectID, []models.ProjectConfiguration{setting})
}

// AssignApplicationsDirect attaches a project directly to one or more
// applications (DIRECT_APP_ASSOCIATION_ENABLED feature flag required).
// POST /api/projects/{id}/applications → 201.
func AssignApplicationsDirect(ctx context.Context, e *transport.Executor, baseURL, projectID string, applicationIDs []string) error {
	body := models.ProjectApplicationsModel{ApplicationIDs: applicationIDs}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+projectID+"/applications"),
		nil, body, []int{http.StatusCreated, http.StatusOK, http.StatusNoContent}, nil)
}

// RemoveApplicationsDirect detaches a project from one or more applications.
// DELETE /api/projects/{id}/applications → 204.
func RemoveApplicationsDirect(ctx context.Context, e *transport.Executor, baseURL, projectID string, applicationIDs []string) error {
	body := models.ProjectApplicationsModel{ApplicationIDs: applicationIDs}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path+"/"+projectID+"/applications"),
		nil, body, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// MoveApplications reassigns a project from one set of applications to
// another in a single API call.
// PUT /api/projects/reassign/{id} → 204.
func MoveApplications(ctx context.Context, e *transport.Executor, baseURL, projectID string, fromAppIDs, toAppIDs []string) error {
	if projectID == "" {
		return &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	body := struct {
		Source []string `json:"applicationIdsToDisassociate"`
		Dest   []string `json:"applicationIdsToAssociate"`
	}{Source: fromAppIDs, Dest: toAppIDs}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(baseURL, Path+"/reassign/"+projectID),
		nil, body, []int{http.StatusNoContent, http.StatusOK}, nil)
}
