package access

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// GetMyGroups returns groups visible to the current caller (IAM Phase 2).
// GET /api/access-management/my-groups?search=&subgroups=&limit=&offset= → 200.
func GetMyGroups(ctx context.Context, e *transport.Executor, baseURL, search string, subgroups bool, limit, offset int) ([]models.Group, error) {
	q := url.Values{}
	if search != "" {
		q.Set("search", search)
	}
	q.Set("subgroups", strconv.FormatBool(subgroups))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out []models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/my-groups"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAvailableGroups returns groups available for assignment to a project.
// GET /api/access-management/available-groups?project-id=&search=&limit=&offset= → 200.
func GetAvailableGroups(ctx context.Context, e *transport.Executor, baseURL, projectID, search string, limit, offset int) ([]models.Group, error) {
	q := url.Values{}
	if projectID != "" {
		q.Set("project-id", projectID)
	}
	if search != "" {
		q.Set("search", search)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out []models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/available-groups"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAMGroups returns groups via the access-management filter endpoint.
// GET /api/access-management/groups?search=&limit=&offset= → 200.
func ListAMGroups(ctx context.Context, e *transport.Executor, baseURL, search string, limit, offset int) ([]models.Group, error) {
	return listAM[models.Group](ctx, e, baseURL, "groups", search, limit, offset)
}

// ListAMUsers returns users via the access-management filter endpoint.
// GET /api/access-management/users?search=&limit=&offset= → 200.
func ListAMUsers(ctx context.Context, e *transport.Executor, baseURL, search string, limit, offset int) ([]models.User, error) {
	return listAM[models.User](ctx, e, baseURL, "users", search, limit, offset)
}

// ListAMClients returns OIDC clients via the access-management filter endpoint.
// GET /api/access-management/clients?search=&limit=&offset= → 200.
func ListAMClients(ctx context.Context, e *transport.Executor, baseURL, search string, limit, offset int) ([]models.OIDCClient, error) {
	return listAM[models.OIDCClient](ctx, e, baseURL, "clients", search, limit, offset)
}

// ListAMApplications returns applications via the access-management filter.
// GET /api/access-management/applications?search=&limit=&offset= → 200.
func ListAMApplications(ctx context.Context, e *transport.Executor, baseURL, search string, limit, offset int) ([]models.Application, error) {
	return listAM[models.Application](ctx, e, baseURL, "applications", search, limit, offset)
}

// ListAMProjects returns projects via the access-management filter.
// GET /api/access-management/projects?search=&limit=&offset= → 200.
func ListAMProjects(ctx context.Context, e *transport.Executor, baseURL, search string, limit, offset int) ([]models.ProjectResponseModel, error) {
	return listAM[models.ProjectResponseModel](ctx, e, baseURL, "projects", search, limit, offset)
}

// ListPermissions returns the AM permission catalog.
// GET /api/access-management/permissions → 200.
func ListPermissions(ctx context.Context, e *transport.Executor, baseURL string) ([]models.AMPermission, error) {
	var out []models.AMPermission
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/permissions"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAMRoles returns the AM role catalog.
// GET /api/access-management/roles → 200.
func ListAMRoles(ctx context.Context, e *transport.Executor, baseURL string) ([]models.AMRole, error) {
	var out []models.AMRole
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/roles"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func listAM[T any](ctx context.Context, e *transport.Executor, baseURL, resource, search string, limit, offset int) ([]T, error) {
	q := url.Values{}
	if search != "" {
		q.Set("search", search)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out []T
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+resource),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
