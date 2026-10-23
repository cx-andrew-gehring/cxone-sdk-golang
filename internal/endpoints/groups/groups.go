// Package groups implements the Checkmarx One IAM groups lookup endpoint.
//
// Note: this endpoint lives under the IAM path prefix (auth/realms/...) on
// the same host as the API. The tenant placeholder is substituted at call
// time. CLAUDE.md does not call out groups specifically; it lives here for
// completeness with the rest of the low-level layer.
package groups

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// PathTemplate is the relative path with the tenant placeholder. The literal
// string "organization" is what the platform uses as the placeholder; the
// SDK substitutes the actual tenant before sending the request.
const PathTemplate = "auth/realms/organization/pip/groups"

// List returns groups visible to the caller, optionally filtered by name.
// GET /auth/realms/{tenant}/pip/groups?groupName={name} → 200.
//
// tenant is required and is substituted into the path template.
func List(ctx context.Context, e *transport.Executor, baseURL, tenant, groupName string) ([]models.Group, error) {
	if tenant == "" {
		return nil, &cxerrors.ConfigurationError{Field: "tenant", Reason: "is required"}
	}
	path := strings.Replace(PathTemplate, "organization", strings.ToLower(tenant), 1)
	q := url.Values{}
	if groupName != "" {
		q.Set("groupName", groupName)
	}
	var out []models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, path),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Admin-realm endpoints (Keycloak)
//
// These target the IAM admin URL supplied by advanced.Handle — not the API
// host — and give full CRUD + hierarchy + role-binding access to the tenant
// groups resource. The legacy PIP-based List above continues to exist for
// backward compatibility but only covers a flat lookup.
// ---------------------------------------------------------------------------

// AdminPath is the relative path to the groups collection under the tenant
// admin realm.
const AdminPath = "groups"

// ListAdmin returns groups matching filter. GET /groups → 200.
func ListAdmin(ctx context.Context, e *transport.Executor, iamAdminURL string, filter models.GroupFilter) ([]models.Group, error) {
	var out []models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, AdminPath),
		buildQuery(filter, false), nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Count returns the number of groups matching filter. GET /groups/count → 200.
//
// Keycloak accepts only "search" and "top" parameters on the count endpoint,
// so the other [models.GroupFilter] fields are ignored here.
func Count(ctx context.Context, e *transport.Executor, iamAdminURL string, filter models.GroupFilter) (uint64, error) {
	var out models.GroupCountResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, AdminPath+"/count"),
		buildQuery(filter, true), nil, []int{http.StatusOK}, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

// GetByID fetches a single group by uuid, with roles populated.
// GET /groups/{id}?briefRepresentation=false → 200.
//
// Note: recent Keycloak versions return immediate children in the
// subGroupCount field but do not include the subGroups array — use
// [GetChildren] to populate it.
func GetByID(ctx context.Context, e *transport.Executor, iamAdminURL, groupID string) (*models.Group, error) {
	if groupID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	q := url.Values{}
	q.Set("briefRepresentation", "false")
	var out models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetByPath fetches a group by its tenant-absolute path.
// GET /group-by-path/{path} → 200.
//
// Leading slashes are stripped from path.
func GetByPath(ctx context.Context, e *transport.Executor, iamAdminURL, path string) (*models.Group, error) {
	if path == "" {
		return nil, &cxerrors.ConfigurationError{Field: "path", Reason: "is required"}
	}
	p := strings.TrimPrefix(path, "/")
	var out models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "group-by-path/"+p),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetChildren returns the immediate sub-groups of a parent group.
// GET /groups/{id}/children?first={first}&max={max}&briefRepresentation=false → 200.
func GetChildren(ctx context.Context, e *transport.Executor, iamAdminURL, groupID string, first, max int) ([]models.Group, error) {
	if groupID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	q := url.Values{}
	q.Set("briefRepresentation", "false")
	if first > 0 {
		q.Set("first", strconv.Itoa(first))
	}
	if max > 0 {
		q.Set("max", strconv.Itoa(max))
	}
	var out []models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/children"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateAdmin creates a top-level group. POST /groups → 201 (Location header
// with the new uuid). Returns the fully-populated group record.
func CreateAdmin(ctx context.Context, e *transport.Executor, iamAdminURL string, in *models.Group) (*models.Group, error) {
	if in == nil || in.Name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "group.Name", Reason: "is required"}
	}
	id, err := createAndExtractID(ctx, e, iamAdminURL, AdminPath, in)
	if err != nil {
		return nil, err
	}
	return GetByID(ctx, e, iamAdminURL, id)
}

// CreateChild creates a new child of an existing group. Keycloak returns the
// created group in the response body directly.
// POST /groups/{parentID}/children → 201.
func CreateChild(ctx context.Context, e *transport.Executor, iamAdminURL, parentID string, in *models.Group) (*models.Group, error) {
	if parentID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "parentID", Reason: "is required"}
	}
	if in == nil || in.Name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "group.Name", Reason: "is required"}
	}
	var out models.Group
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+parentID+"/children"),
		nil, in, []int{http.StatusCreated, http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAdmin overwrites a group. PUT /groups/{id} → 204.
func UpdateAdmin(ctx context.Context, e *transport.Executor, iamAdminURL string, g *models.Group) error {
	if g == nil || g.ID == "" {
		return &cxerrors.ConfigurationError{Field: "group.ID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+g.ID),
		nil, g, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// DeleteAdmin removes a group by uuid. DELETE /groups/{id} → 204.
func DeleteAdmin(ctx context.Context, e *transport.Executor, iamAdminURL, groupID string) error {
	if groupID == "" {
		return &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetMembers returns the users who belong to a group.
// GET /groups/{id}/members → 200.
func GetMembers(ctx context.Context, e *transport.Executor, iamAdminURL, groupID string) ([]models.User, error) {
	if groupID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	var out []models.User
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/members"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRealmRoles returns the realm roles directly bound to a group.
// GET /groups/{id}/role-mappings/realm → 200.
func GetRealmRoles(ctx context.Context, e *transport.Executor, iamAdminURL, groupID string) ([]models.Role, error) {
	if groupID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/role-mappings/realm"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddRealmRoles binds realm roles to a group.
// POST /groups/{id}/role-mappings/realm → 204.
func AddRealmRoles(ctx context.Context, e *transport.Executor, iamAdminURL, groupID string, roles []models.Role) error {
	if groupID == "" {
		return &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/role-mappings/realm"),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// RemoveRealmRoles unbinds realm roles from a group.
// DELETE /groups/{id}/role-mappings/realm → 204.
func RemoveRealmRoles(ctx context.Context, e *transport.Executor, iamAdminURL, groupID string, roles []models.Role) error {
	if groupID == "" {
		return &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/role-mappings/realm"),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetClientRoles returns client-scoped roles bound to a group under the
// named Keycloak client. GET /groups/{id}/role-mappings/clients/{clientID} → 200.
func GetClientRoles(ctx context.Context, e *transport.Executor, iamAdminURL, groupID, clientID string) ([]models.Role, error) {
	if groupID == "" || clientID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "groupID/clientID", Reason: "both are required"}
	}
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/role-mappings/clients/"+clientID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddClientRoles binds client-scoped roles to a group.
// POST /groups/{id}/role-mappings/clients/{clientID} → 204.
func AddClientRoles(ctx context.Context, e *transport.Executor, iamAdminURL, groupID, clientID string, roles []models.Role) error {
	if groupID == "" || clientID == "" {
		return &cxerrors.ConfigurationError{Field: "groupID/clientID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/role-mappings/clients/"+clientID),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// RemoveClientRoles unbinds client-scoped roles from a group.
// DELETE /groups/{id}/role-mappings/clients/{clientID} → 204.
func RemoveClientRoles(ctx context.Context, e *transport.Executor, iamAdminURL, groupID, clientID string, roles []models.Role) error {
	if groupID == "" || clientID == "" {
		return &cxerrors.ConfigurationError{Field: "groupID/clientID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, AdminPath+"/"+groupID+"/role-mappings/clients/"+clientID),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

func buildQuery(f models.GroupFilter, forCount bool) url.Values {
	q := url.Values{}
	if f.Search != "" {
		q.Set("search", f.Search)
	}
	if forCount {
		if f.TopLevel {
			q.Set("top", "true")
		}
		return q
	}
	if f.First > 0 {
		q.Set("first", strconv.Itoa(f.First))
	}
	if f.Max > 0 {
		q.Set("max", strconv.Itoa(f.Max))
	}
	if f.BriefRepresentation != nil {
		q.Set("briefRepresentation", strconv.FormatBool(*f.BriefRepresentation))
	}
	if f.PopulateHierarchy != nil {
		q.Set("populateHierarchy", strconv.FormatBool(*f.PopulateHierarchy))
	}
	if f.Exact != nil {
		q.Set("exact", strconv.FormatBool(*f.Exact))
	}
	return q
}

// createAndExtractID POSTs a body and returns the id parsed from the 201
// Location header.
func createAndExtractID(ctx context.Context, e *transport.Executor, iamAdminURL, subPath string, in any) (string, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return "", &cxerrors.ConfigurationError{Field: "body", Reason: "json marshal: " + err.Error()}
	}
	req := &transport.Request{
		Method:      http.MethodPost,
		URL:         transport.JoinURL(iamAdminURL, subPath),
		Body:        bytes.NewReader(body),
		ContentType: "application/json",
	}
	resp, err := e.Do(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", &cxerrors.ResponseError{
			Method: http.MethodPost, URL: req.URL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        fmt.Sprintf("unexpected status %d", resp.StatusCode),
		}
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", &cxerrors.ResponseError{
			Method: http.MethodPost, URL: req.URL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        "create: missing Location header",
		}
	}
	idx := strings.LastIndex(location, "/")
	if idx < 0 || idx+1 >= len(location) {
		return "", &cxerrors.ResponseError{
			Method: http.MethodPost, URL: req.URL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        "create: malformed Location header: " + location,
		}
	}
	return location[idx+1:], nil
}
