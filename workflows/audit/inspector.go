package audit

import (
	"context"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/audit"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// SessionInspector wraps an [models.AuditSession] with typed status
// properties, modeled after the ScanInspector pattern (CLAUDE.md §9.4).
type SessionInspector struct {
	backend *Backend
	session *models.AuditSession
}

// FromSession wraps an already-created session into an inspector.
func FromSession(backend *Backend, session *models.AuditSession) (*SessionInspector, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if session == nil {
		return nil, &cxerrors.ConfigurationError{Field: "session", Reason: "is required"}
	}
	return &SessionInspector{backend: backend, session: session}, nil
}

// Session returns the underlying session handle.
func (i *SessionInspector) Session() *models.AuditSession { return i.session }

// ID returns the session id.
func (i *SessionInspector) ID() string { return i.session.ID }

// Status returns the raw session status string from the platform.
func (i *SessionInspector) Status() string { return i.session.Data.Status }

// Allocated returns true when the session is in the "ALLOCATED" state
// (SAST sessions report this after the initial creation).
func (i *SessionInspector) Allocated() bool { return i.session.Data.Status == "ALLOCATED" }

// Running returns true when the session is in the "RUNNING" state.
func (i *SessionInspector) Running() bool { return i.session.Data.Status == "RUNNING" }

// KeepAlive sends a heartbeat to prevent the session from expiring. Safe to
// call repeatedly; the endpoint is rate-limited server-side.
func (i *SessionInspector) KeepAlive(ctx context.Context) error {
	return endpoints.KeepAlive(ctx, i.backend.Executor, i.backend.BaseURL, i.session.ID)
}

// Delete releases the session, freeing the slot for a new one.
func (i *SessionInspector) Delete(ctx context.Context) error {
	return endpoints.DeleteSession(ctx, i.backend.Executor, i.backend.BaseURL, i.session.ID)
}

// PollRequest checks whether the asynchronous request identified by
// requestID has completed. Returns the status model which includes
// Completed, Value, and any error details.
func (i *SessionInspector) PollRequest(ctx context.Context, requestID string) (*models.AuditRequestStatus, error) {
	return endpoints.GetRequestStatus(ctx, i.backend.Executor, i.backend.BaseURL, i.session.ID, requestID)
}
