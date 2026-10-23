// Package users implements the Checkmarx One IAM user administration API.
//
// These endpoints live under the Keycloak admin realm path
// ("/auth/admin/realms/{tenant}/users/..."), which is on the IAM host — not
// the API host. The advanced.Handle supplies a pre-computed iamAdminURL to
// every function in this package so callers don't need to know about host
// discrimination.
//
// This package is part of the SDK's low-level endpoint layer (CLAUDE.md §8).
// High-level helpers (GetOrCreate, lookups by email/username that tolerate
// multiple matches, lazy group/role fill) live in workflows/users.
package users

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

// Path is the user-administration sub-path under the tenant admin realm
// (e.g. "https://iam.checkmarx.net/auth/admin/realms/<tenant>/" + Path).
const Path = "users"

// List returns users matching filter. GET /users → 200.
//
// The Keycloak response is a bare JSON array; callers receive it as a Go
// slice. Unknown fields in individual user entries are tolerated.
func List(ctx context.Context, e *transport.Executor, iamAdminURL string, filter models.UserFilter) ([]models.User, error) {
	var out []models.User
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path),
		buildQuery(filter, false), nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Count returns the number of users matching filter. GET /users/count → 200.
//
// Keycloak returns the count as a bare integer body, not an object.
func Count(ctx context.Context, e *transport.Executor, iamAdminURL string, filter models.UserFilter) (uint64, error) {
	// Keycloak /users/count rejects some filter fields (briefRepresentation,
	// exact, idpAlias). Build a count-specific query that omits them.
	finalURL, err := appendRawQuery(transport.JoinURL(iamAdminURL, Path+"/count"), buildQuery(filter, true))
	if err != nil {
		return 0, &cxerrors.ConfigurationError{Field: "url", Reason: err.Error()}
	}
	req := &transport.Request{Method: http.MethodGet, URL: finalURL}
	resp, err := e.Do(ctx, req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	if err != nil {
		return 0, &cxerrors.ResponseError{
			Method: http.MethodGet, URL: finalURL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        "read count body: " + err.Error(), Cause: err,
		}
	}
	if resp.StatusCode != http.StatusOK {
		return 0, &cxerrors.ResponseError{
			Method: http.MethodGet, URL: finalURL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        fmt.Sprintf("unexpected status %d", resp.StatusCode),
		}
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(body)), 10, 64)
	if err != nil {
		return 0, &cxerrors.ResponseError{
			Method: http.MethodGet, URL: finalURL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        "parse count body: " + err.Error(), Cause: err,
		}
	}
	return n, nil
}

// Get fetches a single user by id. GET /users/{id}?briefRepresentation=false → 200.
func Get(ctx context.Context, e *transport.Executor, iamAdminURL, userID string) (*models.User, error) {
	if userID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "userID", Reason: "is required"}
	}
	q := url.Values{}
	q.Set("briefRepresentation", "false")
	var out models.User
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path+"/"+userID),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Create creates a new user. POST /users → 201 Created. Keycloak returns the
// new user's id only via the Location header ("/users/<uuid>"); Create parses
// that and issues a follow-up [Get] so callers receive the fully-populated
// user record in one call.
func Create(ctx context.Context, e *transport.Executor, iamAdminURL string, in *models.User) (*models.User, error) {
	if in == nil {
		return nil, &cxerrors.ConfigurationError{Field: "user", Reason: "is required"}
	}
	id, err := createAndExtractID(ctx, e, iamAdminURL, in)
	if err != nil {
		return nil, err
	}
	return Get(ctx, e, iamAdminURL, id)
}

// CreateSAML creates a user that logs in via SAML. This is a two-step dance:
// first POST the user with federatedIdentities + totp=false, then GET it,
// clear requiredActions, and PUT the cleaned-up payload back.
//
// idpAlias is the Keycloak identity-provider alias; idpUserID and idpUserName
// are the external IdP's identifiers for the user.
func CreateSAML(ctx context.Context, e *transport.Executor, iamAdminURL string, in *models.User, idpAlias, idpUserID, idpUserName string) (*models.User, error) {
	if in == nil {
		return nil, &cxerrors.ConfigurationError{Field: "user", Reason: "is required"}
	}
	if idpAlias == "" || idpUserID == "" || idpUserName == "" {
		return nil, &cxerrors.ConfigurationError{Field: "idp*", Reason: "idpAlias, idpUserID, and idpUserName are required"}
	}
	// Clone and set federated identity + totp.
	payload := *in
	falsy := false
	payload.Totp = &falsy
	payload.FederatedIdentities = []models.UserFederatedIdentity{{
		IdentityProvider: idpAlias,
		UserID:           idpUserID,
		UserName:         idpUserName,
	}}
	id, err := createAndExtractID(ctx, e, iamAdminURL, &payload)
	if err != nil {
		return nil, err
	}
	// Fetch, scrub requiredActions, PUT back.
	fetched, err := Get(ctx, e, iamAdminURL, id)
	if err != nil {
		return nil, err
	}
	fetched.RequiredActions = []string{}
	if err := Update(ctx, e, iamAdminURL, fetched); err != nil {
		return nil, err
	}
	return Get(ctx, e, iamAdminURL, id)
}

// Update overwrites a user. PUT /users/{id} → 204.
//
// The full User object is sent; fields omitted from user will be cleared on
// the server. Callers that want a partial update should [Get] first, mutate,
// then Update.
func Update(ctx context.Context, e *transport.Executor, iamAdminURL string, user *models.User) error {
	if user == nil || user.ID == "" {
		return &cxerrors.ConfigurationError{Field: "user.ID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(iamAdminURL, Path+"/"+user.ID),
		nil, user, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// Delete removes a user by id. DELETE /users/{id} → 204.
func Delete(ctx context.Context, e *transport.Executor, iamAdminURL, userID string) error {
	if userID == "" {
		return &cxerrors.ConfigurationError{Field: "userID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, Path+"/"+userID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetGroups returns the groups a user is a member of.
// GET /users/{id}/groups → 200.
func GetGroups(ctx context.Context, e *transport.Executor, iamAdminURL, userID string) ([]models.Group, error) {
	if userID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "userID", Reason: "is required"}
	}
	var out []models.Group
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/groups"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AssignGroup adds a user to a group. PUT /users/{id}/groups/{groupID} → 204.
func AssignGroup(ctx context.Context, e *transport.Executor, iamAdminURL, userID, groupID string) error {
	if userID == "" || groupID == "" {
		return &cxerrors.ConfigurationError{Field: "userID/groupID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/groups/"+groupID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// RemoveGroup removes a user from a group. DELETE /users/{id}/groups/{groupID} → 204.
func RemoveGroup(ctx context.Context, e *transport.Executor, iamAdminURL, userID, groupID string) error {
	if userID == "" || groupID == "" {
		return &cxerrors.ConfigurationError{Field: "userID/groupID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/groups/"+groupID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetClientRoles returns the roles assigned to a user for a specific
// Keycloak client (Checkmarx One app roles live under the "ast-app" client).
// GET /users/{id}/role-mappings/clients/{clientID} → 200.
func GetClientRoles(ctx context.Context, e *transport.Executor, iamAdminURL, userID, clientID string) ([]models.Role, error) {
	if userID == "" || clientID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "userID/clientID", Reason: "both are required"}
	}
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/role-mappings/clients/"+clientID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddClientRoles grants roles for the given client to a user.
// POST /users/{id}/role-mappings/clients/{clientID} → 204.
func AddClientRoles(ctx context.Context, e *transport.Executor, iamAdminURL, userID, clientID string, roles []models.Role) error {
	if userID == "" || clientID == "" {
		return &cxerrors.ConfigurationError{Field: "userID/clientID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/role-mappings/clients/"+clientID),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// RemoveClientRoles revokes roles for the given client from a user.
// DELETE /users/{id}/role-mappings/clients/{clientID} → 204.
func RemoveClientRoles(ctx context.Context, e *transport.Executor, iamAdminURL, userID, clientID string, roles []models.Role) error {
	if userID == "" || clientID == "" {
		return &cxerrors.ConfigurationError{Field: "userID/clientID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/role-mappings/clients/"+clientID),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetRealmRoles returns the realm (IAM) roles directly assigned to a user.
// GET /users/{id}/role-mappings/realm → 200.
func GetRealmRoles(ctx context.Context, e *transport.Executor, iamAdminURL, userID string) ([]models.Role, error) {
	if userID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "userID", Reason: "is required"}
	}
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/role-mappings/realm"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddRealmRoles grants realm roles to a user.
// POST /users/{id}/role-mappings/realm → 204.
func AddRealmRoles(ctx context.Context, e *transport.Executor, iamAdminURL, userID string, roles []models.Role) error {
	if userID == "" {
		return &cxerrors.ConfigurationError{Field: "userID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/role-mappings/realm"),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// RemoveRealmRoles revokes realm roles from a user.
// DELETE /users/{id}/role-mappings/realm → 204.
func RemoveRealmRoles(ctx context.Context, e *transport.Executor, iamAdminURL, userID string, roles []models.Role) error {
	if userID == "" {
		return &cxerrors.ConfigurationError{Field: "userID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, Path+"/"+userID+"/role-mappings/realm"),
		nil, roles, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// buildQuery renders a [models.UserFilter] as url.Values. When forCount is
// true, the Keycloak /users/count endpoint is targeted and the fields it
// rejects (briefRepresentation, exact, idpAlias, idpUserId) are suppressed.
func buildQuery(f models.UserFilter, forCount bool) url.Values {
	q := url.Values{}
	if f.First > 0 {
		q.Set("first", strconv.Itoa(f.First))
	}
	if f.Max > 0 {
		q.Set("max", strconv.Itoa(f.Max))
	}
	if f.Email != "" {
		q.Set("email", f.Email)
	}
	if f.FirstName != "" {
		q.Set("firstName", f.FirstName)
	}
	if f.Username != "" {
		q.Set("username", f.Username)
	}
	if f.Q != "" {
		q.Set("q", f.Q)
	}
	if f.Search != "" {
		q.Set("search", f.Search)
	}
	if f.EmailVerified != nil {
		q.Set("emailVerified", strconv.FormatBool(*f.EmailVerified))
	}
	if f.Enabled != nil {
		q.Set("enabled", strconv.FormatBool(*f.Enabled))
	}
	if !forCount {
		if f.BriefRepresentation != nil {
			q.Set("briefRepresentation", strconv.FormatBool(*f.BriefRepresentation))
		}
		if f.Exact != nil {
			q.Set("exact", strconv.FormatBool(*f.Exact))
		}
		if f.IDPAlias != "" {
			q.Set("idpAlias", f.IDPAlias)
		}
		if f.IDPUserID != "" {
			q.Set("idpUserId", f.IDPUserID)
		}
	}
	return q
}

// createAndExtractID POSTs a user and returns the id parsed from the 201
// Location header.
func createAndExtractID(ctx context.Context, e *transport.Executor, iamAdminURL string, in *models.User) (string, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return "", &cxerrors.ConfigurationError{Field: "user", Reason: "json marshal: " + err.Error()}
	}
	req := &transport.Request{
		Method:      http.MethodPost,
		URL:         transport.JoinURL(iamAdminURL, Path),
		Body:        bytes.NewReader(body),
		ContentType: "application/json",
	}
	resp, err := e.Do(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// Drain the body to return the connection to the pool.
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
			Reason:        "Create: missing Location header",
		}
	}
	idx := strings.LastIndex(location, "/")
	if idx < 0 || idx+1 >= len(location) {
		return "", &cxerrors.ResponseError{
			Method: http.MethodPost, URL: req.URL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        "Create: malformed Location header: " + location,
		}
	}
	return location[idx+1:], nil
}

// appendRawQuery is a tiny local clone of transport.appendQuery (which is
// unexported). Keeps the package free of transport-internal dependencies.
func appendRawQuery(u string, q url.Values) (string, error) {
	if len(q) == 0 {
		return u, nil
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	existing := parsed.Query()
	for k, vs := range q {
		for _, v := range vs {
			existing.Add(k, v)
		}
	}
	parsed.RawQuery = existing.Encode()
	return parsed.String(), nil
}
