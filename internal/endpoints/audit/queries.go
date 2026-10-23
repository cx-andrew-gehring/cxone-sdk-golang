package audit

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// GetSASTQueries returns the SAST query collection for the session.
// GET /api/query-editor/sessions/{id}/queries?projectId=X → 200.
func GetSASTQueries(ctx context.Context, e *transport.Executor, baseURL, sessionID, level, levelID string) (*models.SASTQueryCollection, error) {
	if sessionID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	q := url.Values{}
	if level != "" {
		q.Set("level", level)
	}
	if levelID != "" {
		q.Set("projectId", levelID)
	}
	var out models.SASTQueryCollection
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetIACQueries returns the IAC query collection for the session.
// GET /api/query-editor/sessions/{id}/queries?projectId=X → 200.
func GetIACQueries(ctx context.Context, e *transport.Executor, baseURL, sessionID, level, levelID string) (*models.IACQueryCollection, error) {
	if sessionID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	q := url.Values{}
	if level != "" {
		q.Set("level", level)
	}
	if levelID != "" {
		q.Set("projectId", levelID)
	}
	var out models.IACQueryCollection
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetQueryTree returns the hierarchical query tree for the session.
// GET /api/query-editor/sessions/{id}/queries → 200 (parsed as tree).
func GetQueryTree(ctx context.Context, e *transport.Executor, baseURL, sessionID, level, levelID string) ([]models.AuditQueryTree, error) {
	if sessionID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	q := url.Values{}
	if level != "" {
		q.Set("level", level)
	}
	if levelID != "" {
		q.Set("projectId", levelID)
	}
	var out []models.AuditQueryTree
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteQueryOverride removes a query override by its editor key.
// DELETE /api/query-editor/sessions/{id}/queries/{key} → 204.
func DeleteQueryOverride(ctx context.Context, e *transport.Executor, baseURL, sessionID, queryKey string) error {
	if sessionID == "" || queryKey == "" {
		return &cxerrors.ConfigurationError{Field: "sessionID/queryKey", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries/"+url.PathEscape(queryKey)),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// CreateQueryOverride creates or overrides a query in the session.
// POST /api/query-editor/sessions/{id}/queries → 200/201.
func CreateQueryOverride(ctx context.Context, e *transport.Executor, baseURL, sessionID string, body any) error {
	if sessionID == "" {
		return &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries"),
		nil, body, []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}, nil)
}

// UpdateQuerySource updates the source of a query in the session.
// POST /api/query-editor/sessions/{id}/queries/{key}/source → 200.
func UpdateQuerySource(ctx context.Context, e *transport.Executor, baseURL, sessionID, queryKey string, body any) ([]models.QueryFailure, error) {
	if sessionID == "" || queryKey == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID/queryKey", Reason: "both are required"}
	}
	var out []models.QueryFailure
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries/"+url.PathEscape(queryKey)+"/source"),
		nil, body, []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateQueryMetadata updates the metadata of a query in the session.
// PUT /api/query-editor/sessions/{id}/queries/{key}/metadata → 204.
func UpdateQueryMetadata(ctx context.Context, e *transport.Executor, baseURL, sessionID, queryKey string, body any) error {
	if sessionID == "" || queryKey == "" {
		return &cxerrors.ConfigurationError{Field: "sessionID/queryKey", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries/"+url.PathEscape(queryKey)+"/metadata"),
		nil, body, []int{http.StatusOK, http.StatusNoContent}, nil)
}

// ValidateQuerySource validates query source code without saving it.
// POST /api/query-editor/sessions/{id}/queries/validate → 200.
func ValidateQuerySource(ctx context.Context, e *transport.Executor, baseURL, sessionID string, body any) ([]models.QueryFailure, error) {
	if sessionID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	var out []models.QueryFailure
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries/validate"),
		nil, body, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RunQuery executes a query against the session's scan data and returns
// any failures.
// POST /api/query-editor/sessions/{id}/queries/run → 200.
func RunQuery(ctx context.Context, e *transport.Executor, baseURL, sessionID string, body any) (*models.QueryFailure, error) {
	if sessionID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	var out models.QueryFailure
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/queries/run"),
		nil, body, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
