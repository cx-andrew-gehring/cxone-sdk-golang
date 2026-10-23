// Package clients implements the Checkmarx One OIDC client administration
// API. These are the Keycloak clients used by the platform itself ("ast-app")
// and by tenant-created service accounts for API automation.
//
// All calls target the tenant's Keycloak admin realm URL supplied by the
// advanced handle. Part of the SDK's low-level endpoint layer (CLAUDE.md §8).
package clients

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the clients collection under the tenant admin
// realm.
const Path = "clients"

// List returns OIDC clients matching filter. GET /clients → 200.
//
// Keycloak returns a bare JSON array. Callers who don't need filtering may
// pass an empty [models.OIDCClientFilter].
func List(ctx context.Context, e *transport.Executor, iamAdminURL string, filter models.OIDCClientFilter) ([]models.OIDCClient, error) {
	var out []models.OIDCClient
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path),
		buildQuery(filter), nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches a single OIDC client by uuid. GET /clients/{id} → 200.
func Get(ctx context.Context, e *transport.Executor, iamAdminURL, clientUUID string) (*models.OIDCClient, error) {
	if clientUUID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "clientUUID", Reason: "is required"}
	}
	var out models.OIDCClient
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path+"/"+clientUUID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Create creates a new OIDC client. POST /clients → 201.
//
// Keycloak does not return the created object in the response body — it sets
// a Location header. For convenience, this function simply validates the
// request and posts; callers should then issue a follow-up [List] with a
// matching clientId filter to retrieve the new uuid, or use the workflow
// helper in workflows/clients.
func Create(ctx context.Context, e *transport.Executor, iamAdminURL string, in *models.OIDCClient) error {
	if in == nil || in.ClientID == "" {
		return &cxerrors.ConfigurationError{Field: "client.ClientID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, Path),
		nil, in, []int{http.StatusCreated, http.StatusOK, http.StatusNoContent}, nil)
}

// Update overwrites an OIDC client. PUT /clients/{id} → 204.
func Update(ctx context.Context, e *transport.Executor, iamAdminURL string, in *models.OIDCClient) error {
	if in == nil || in.ID == "" {
		return &cxerrors.ConfigurationError{Field: "client.ID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(iamAdminURL, Path+"/"+in.ID),
		nil, in, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// Delete removes an OIDC client by uuid. DELETE /clients/{id} → 204.
//
// Callers must NOT delete the built-in "ast-app" client — doing so will break
// the tenant. The SDK does not enforce this at the low level; workflows do.
func Delete(ctx context.Context, e *transport.Executor, iamAdminURL, clientUUID string) error {
	if clientUUID == "" {
		return &cxerrors.ConfigurationError{Field: "clientUUID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, Path+"/"+clientUUID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetSecret returns the current client secret for an OIDC client. Only the
// creator of a service-account client can read its secret.
// GET /clients/{id}/client-secret → 200.
func GetSecret(ctx context.Context, e *transport.Executor, iamAdminURL, clientUUID string) (*models.ClientSecretResponse, error) {
	if clientUUID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "clientUUID", Reason: "is required"}
	}
	var out models.ClientSecretResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path+"/"+clientUUID+"/client-secret"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RegenerateSecret rotates the client secret.
// POST /clients/{id}/client-secret → 200.
func RegenerateSecret(ctx context.Context, e *transport.Executor, iamAdminURL, clientUUID string) (*models.ClientSecretResponse, error) {
	if clientUUID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "clientUUID", Reason: "is required"}
	}
	var out models.ClientSecretResponse
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, Path+"/"+clientUUID+"/client-secret"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddDefaultScope attaches a client-scope to a client.
// PUT /clients/{id}/default-client-scopes/{scopeID} → 204.
func AddDefaultScope(ctx context.Context, e *transport.Executor, iamAdminURL, clientUUID, scopeID string) error {
	if clientUUID == "" || scopeID == "" {
		return &cxerrors.ConfigurationError{Field: "clientUUID/scopeID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(iamAdminURL, Path+"/"+clientUUID+"/default-client-scopes/"+scopeID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// RemoveDefaultScope detaches a client-scope from a client.
// DELETE /clients/{id}/default-client-scopes/{scopeID} → 204.
func RemoveDefaultScope(ctx context.Context, e *transport.Executor, iamAdminURL, clientUUID, scopeID string) error {
	if clientUUID == "" || scopeID == "" {
		return &cxerrors.ConfigurationError{Field: "clientUUID/scopeID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, Path+"/"+clientUUID+"/default-client-scopes/"+scopeID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetServiceAccountUser returns the Keycloak user that backs a service-account
// OIDC client. Role and group assignments for the client flow through this
// user object. GET /clients/{id}/service-account-user → 200.
func GetServiceAccountUser(ctx context.Context, e *transport.Executor, iamAdminURL, clientUUID string) (*models.User, error) {
	if clientUUID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "clientUUID", Reason: "is required"}
	}
	var out models.User
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, Path+"/"+clientUUID+"/service-account-user"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListScopes returns every client-scope defined on the tenant realm.
// GET /client-scopes → 200.
func ListScopes(ctx context.Context, e *transport.Executor, iamAdminURL string) ([]models.OIDCClientScope, error) {
	var out []models.OIDCClientScope
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "client-scopes"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// buildQuery renders a [models.OIDCClientFilter] as url.Values.
func buildQuery(f models.OIDCClientFilter) url.Values {
	q := url.Values{}
	if f.First > 0 {
		q.Set("first", strconv.Itoa(f.First))
	}
	if f.Max > 0 {
		q.Set("max", strconv.Itoa(f.Max))
	}
	if f.ClientID != "" {
		q.Set("clientId", f.ClientID)
	}
	if f.Q != "" {
		q.Set("q", f.Q)
	}
	if f.Search != nil {
		q.Set("search", strconv.FormatBool(*f.Search))
	}
	if f.ViewableOnly != nil {
		q.Set("viewableOnly", strconv.FormatBool(*f.ViewableOnly))
	}
	return q
}
