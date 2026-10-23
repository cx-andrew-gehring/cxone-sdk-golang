// Package audit implements the Checkmarx One query-editor (audit) session
// API. Audit sessions scope query-editor operations to a tenant, project,
// or scan; each session must be kept alive periodically via [KeepAlive] or
// it will expire.
//
// Part of the SDK's low-level endpoint layer (CLAUDE.md §8). The workflow
// layer (workflows/audit, not yet implemented) wraps these primitives with
// an inspector + polling waiter.
package audit

import (
	"context"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the query-editor sessions collection, relative to the API host root.
const Path = "api/query-editor/sessions"

// CreateSession opens an audit session.
//
// For a tenant-level session set req.Scanner + req.Filter (the language or
// "all"). For a scan-scoped session set req.Scanner + req.ProjectID +
// req.ScanID instead. The returned session's Data.Status should be
// "ALLOCATED" (SAST) or "RUNNING" (IAC) before the session is used.
// POST /api/query-editor/sessions → 200/201.
func CreateSession(ctx context.Context, e *transport.Executor, baseURL string, req *models.AuditCreateRequest) (*models.AuditSession, error) {
	if req == nil || req.Scanner == "" {
		return nil, &cxerrors.ConfigurationError{Field: "request.Scanner", Reason: "is required"}
	}
	var out models.AuditSession
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, req, []int{http.StatusOK, http.StatusCreated}, &out); err != nil {
		return nil, err
	}
	out.Engine = req.Scanner
	out.ProjectID = req.ProjectID
	return &out, nil
}

// DeleteSession releases an audit session.
// DELETE /api/query-editor/sessions/{id} → 204.
func DeleteSession(ctx context.Context, e *transport.Executor, baseURL, sessionID string) error {
	if sessionID == "" {
		return &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path+"/"+sessionID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// KeepAlive refreshes an audit session so it does not expire. Checkmarx One
// rate-limits this endpoint; callers should invoke it no more than once per
// two minutes.
// PATCH /api/query-editor/sessions/{id} → 200/204.
func KeepAlive(ctx context.Context, e *transport.Executor, baseURL, sessionID string) error {
	if sessionID == "" {
		return &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPatch,
		transport.JoinURL(baseURL, Path+"/"+sessionID),
		nil, nil, []int{http.StatusOK, http.StatusNoContent}, nil)
}

// GetRequestStatus returns the status of an asynchronous query-editor
// request tied to the session (e.g. the initial language fetch, a scan run,
// or a query override create).
// GET /api/query-editor/sessions/{id}/requests/{requestId} → 200.
func GetRequestStatus(ctx context.Context, e *transport.Executor, baseURL, sessionID, requestID string) (*models.AuditRequestStatus, error) {
	if sessionID == "" || requestID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID/requestID", Reason: "both are required"}
	}
	var out models.AuditRequestStatus
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/requests/"+requestID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetScanSources returns the list of source files attached to a scan-scoped
// audit session.
// GET /api/query-editor/sessions/{id}/sources → 200.
func GetScanSources(ctx context.Context, e *transport.Executor, baseURL, sessionID string) ([]models.AuditScanSourceFile, error) {
	if sessionID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	var out []models.AuditScanSourceFile
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/sources"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RunScan triggers a re-scan against the session's sources. The scan is
// asynchronous; poll [GetRequestStatus] with the returned request id to
// drive it to completion.
// POST /api/query-editor/sessions/{id}/sources/scan → 200/202.
func RunScan(ctx context.Context, e *transport.Executor, baseURL, sessionID string) error {
	if sessionID == "" {
		return &cxerrors.ConfigurationError{Field: "sessionID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"+sessionID+"/sources/scan"),
		nil, nil, []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent}, nil)
}
