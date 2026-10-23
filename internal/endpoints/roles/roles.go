// Package roles implements the Checkmarx One IAM role-administration API.
//
// Checkmarx One exposes two flavors of roles:
//
//   - Realm (IAM) roles — permissions related to user/group administration.
//     These live under /auth/admin/realms/{tenant}/roles.
//   - Client (application) roles — permissions related to Checkmarx One
//     product features (create projects, start scans, etc). These live
//     under /auth/admin/realms/{tenant}/clients/{clientID}/roles, where
//     clientID is the Keycloak client UUID — for Checkmarx One that's the
//     "ast-app" client.
//
// The composites family of endpoints (/roles-by-id/{id}/composites) lets a
// role bundle other roles, which Checkmarx One uses heavily for pre-built
// role presets.
//
// All calls target the IAM admin realm URL supplied by the advanced handle.
// Part of the SDK's low-level endpoint layer (CLAUDE.md §8).
package roles

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// ListRealm returns every realm (IAM) role visible to the caller.
// GET /roles?briefRepresentation=true → 200.
func ListRealm(ctx context.Context, e *transport.Executor, iamAdminURL string) ([]models.Role, error) {
	q := url.Values{}
	q.Set("briefRepresentation", "true")
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "roles"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRealmByName returns a single realm role by name.
// GET /roles/{name} → 200.
func GetRealmByName(ctx context.Context, e *transport.Executor, iamAdminURL, name string) (*models.Role, error) {
	if name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "name", Reason: "is required"}
	}
	var out models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "roles/"+url.PathEscape(name)),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchRealm returns realm roles whose name contains the search term.
// GET /roles?search={q}&briefRepresentation=false → 200.
func SearchRealm(ctx context.Context, e *transport.Executor, iamAdminURL, search string) ([]models.Role, error) {
	q := url.Values{}
	q.Set("search", search)
	q.Set("briefRepresentation", "false")
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "roles"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListClient returns every role defined under a Keycloak client (e.g. the
// ast-app application).
// GET /clients/{clientID}/roles?briefRepresentation=true → 200.
func ListClient(ctx context.Context, e *transport.Executor, iamAdminURL, clientID string) ([]models.Role, error) {
	if clientID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "clientID", Reason: "is required"}
	}
	q := url.Values{}
	q.Set("briefRepresentation", "true")
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "clients/"+clientID+"/roles"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetClientByName returns a single client-scoped role by name.
// GET /clients/{clientID}/roles/{name} → 200.
func GetClientByName(ctx context.Context, e *transport.Executor, iamAdminURL, clientID, name string) (*models.Role, error) {
	if clientID == "" || name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "clientID/name", Reason: "both are required"}
	}
	var out models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "clients/"+clientID+"/roles/"+url.PathEscape(name)),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchClient returns client-scoped roles whose name contains the search term.
// GET /clients/{clientID}/roles?search={q} → 200.
func SearchClient(ctx context.Context, e *transport.Executor, iamAdminURL, clientID, search string) ([]models.Role, error) {
	if clientID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "clientID", Reason: "is required"}
	}
	q := url.Values{}
	q.Set("search", search)
	q.Set("briefRepresentation", "false")
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "clients/"+clientID+"/roles"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateClient creates a new client-scoped role.
// POST /clients/{clientID}/roles → 201.
//
// The caller supplies the full role payload. Checkmarx One custom roles
// usually set Composite=true and ClientRole=true, with Category/Type/Creator
// attributes populated for provenance — see workflows/roles for a builder
// that fills those defaults.
func CreateClient(ctx context.Context, e *transport.Executor, iamAdminURL, clientID string, in *models.Role) error {
	if clientID == "" {
		return &cxerrors.ConfigurationError{Field: "clientID", Reason: "is required"}
	}
	if in == nil || in.Name == "" {
		return &cxerrors.ConfigurationError{Field: "role.Name", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, "clients/"+clientID+"/roles"),
		nil, in, []int{http.StatusCreated, http.StatusOK, http.StatusNoContent}, nil)
}

// GetByID fetches any role (realm or client) by its uuid.
// GET /roles-by-id/{id} → 200.
//
// The returned role does not include sub-roles; use [GetComposites] to fill
// them.
func GetByID(ctx context.Context, e *transport.Executor, iamAdminURL, roleID string) (*models.Role, error) {
	if roleID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "roleID", Reason: "is required"}
	}
	var out models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "roles-by-id/"+roleID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteByID removes a role by its uuid.
// DELETE /roles-by-id/{id} → 204.
func DeleteByID(ctx context.Context, e *transport.Executor, iamAdminURL, roleID string) error {
	if roleID == "" {
		return &cxerrors.ConfigurationError{Field: "roleID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, "roles-by-id/"+roleID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetComposites returns the sub-roles of a composite role.
// GET /roles-by-id/{id}/composites → 200.
func GetComposites(ctx context.Context, e *transport.Executor, iamAdminURL, roleID string) ([]models.Role, error) {
	if roleID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "roleID", Reason: "is required"}
	}
	var out []models.Role
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(iamAdminURL, "roles-by-id/"+roleID+"/composites"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddComposites adds sub-roles to a composite role.
// POST /roles-by-id/{id}/composites → 204.
//
// Only the ID field of each supplied role is sent — the Keycloak API expects
// a compact [{id}] payload. Callers pass [models.Role] values for symmetry
// with the rest of this package.
func AddComposites(ctx context.Context, e *transport.Executor, iamAdminURL, roleID string, subRoles []models.Role) error {
	if roleID == "" {
		return &cxerrors.ConfigurationError{Field: "roleID", Reason: "is required"}
	}
	payload := compactRoleIDs(subRoles)
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(iamAdminURL, "roles-by-id/"+roleID+"/composites"),
		nil, payload, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// RemoveComposites removes sub-roles from a composite role.
// DELETE /roles-by-id/{id}/composites → 204.
func RemoveComposites(ctx context.Context, e *transport.Executor, iamAdminURL, roleID string, subRoles []models.Role) error {
	if roleID == "" {
		return &cxerrors.ConfigurationError{Field: "roleID", Reason: "is required"}
	}
	payload := compactRoleIDs(subRoles)
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(iamAdminURL, "roles-by-id/"+roleID+"/composites"),
		nil, payload, []int{http.StatusNoContent, http.StatusOK}, nil)
}

type roleIDPayload struct {
	ID string `json:"id"`
}

func compactRoleIDs(in []models.Role) []roleIDPayload {
	out := make([]roleIDPayload, len(in))
	for i, r := range in {
		out[i].ID = r.ID
	}
	return out
}
