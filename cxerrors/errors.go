// Package cxerrors holds the SDK's typed error catalog.
//
// The names and semantics are normative across every Checkmarx One SDK
// (CLAUDE.md §7). The root cxone package re-exports these types as aliases so
// callers normally never need to import this package directly:
//
//	var authErr *cxone.AuthError
//	if errors.As(err, &authErr) { ... }
//
// All transport-related errors carry diagnostic fields (HTTP method, sanitized
// URL, status code, attempt, correlation ID) and preserve the underlying cause
// via Unwrap. Bearer tokens and form-body secrets are redacted from every
// message before construction — see internal/redact.
package cxerrors

import (
	"errors"
	"fmt"
	"net/url"
)

// EndpointError is raised when the region or endpoint configuration is
// invalid (bad URL, missing tenant, malformed scheme).
type EndpointError struct {
	Reason string
	Cause  error
}

func (e *EndpointError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("cxone: endpoint configuration invalid: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("cxone: endpoint configuration invalid: %s", e.Reason)
}

func (e *EndpointError) Unwrap() error { return e.Cause }

// AuthError is raised when token acquisition or refresh fails after retries.
// The form body containing client_secret / refresh_token / api_key is never
// included in the message.
type AuthError struct {
	Reason     string
	StatusCode int // 0 if the failure was a transport error, not an HTTP response
	Cause      error
}

func (e *AuthError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("cxone: authentication failed (status %d): %s", e.StatusCode, e.Reason)
	}
	if e.Cause != nil {
		return fmt.Sprintf("cxone: authentication failed: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("cxone: authentication failed: %s", e.Reason)
}

func (e *AuthError) Unwrap() error { return e.Cause }

// CommunicationError is raised when all retries are exhausted on a request,
// or when a non-retryable transport error occurs. It carries the diagnostic
// fields needed to troubleshoot a failed request.
type CommunicationError struct {
	Method        string
	URL           string // already sanitized — no secrets
	StatusCode    int    // 0 if the failure was a transport error, not an HTTP response
	Attempt       int    // attempt number on which the request gave up (1-indexed)
	CorrelationID string
	Cause         error
}

func (e *CommunicationError) Error() string {
	u := redactURL(e.URL)
	if e.StatusCode != 0 {
		return fmt.Sprintf(
			"cxone: %s %s failed after %d attempt(s) with status %d (correlationId=%s)",
			e.Method, u, e.Attempt, e.StatusCode, e.CorrelationID,
		)
	}
	return fmt.Sprintf(
		"cxone: %s %s failed after %d attempt(s) (correlationId=%s): %v",
		e.Method, u, e.Attempt, e.CorrelationID, e.Cause,
	)
}

// redactURL drops the query string, fragment and userinfo (presigned-URL
// signatures, credentials) from a URL before it is printed.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
	return u.String()
}

func (e *CommunicationError) Unwrap() error { return e.Cause }

// ResponseError is raised when a response was received but could not be parsed,
// or the status code was not in the allowed set for that operation.
type ResponseError struct {
	Method        string
	URL           string
	StatusCode    int
	CorrelationID string
	Reason        string
	Cause         error
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf(
		"cxone: %s %s returned an unexpected response (status %d, correlationId=%s): %s",
		e.Method, e.URL, e.StatusCode, e.CorrelationID, e.Reason,
	)
}

func (e *ResponseError) Unwrap() error { return e.Cause }

// ScanError is raised when a high-level scan workflow surfaces a scan-specific
// failure (invalid configuration, scan ended in a failed state when callers
// required success, etc.).
type ScanError struct {
	ScanID string
	Reason string
	Cause  error
}

func (e *ScanError) Error() string {
	if e.ScanID != "" {
		return fmt.Sprintf("cxone: scan %s failed: %s", e.ScanID, e.Reason)
	}
	return fmt.Sprintf("cxone: scan failed: %s", e.Reason)
}

func (e *ScanError) Unwrap() error { return e.Cause }

// ConfigurationError is a runtime validation failure on user-supplied SDK
// configuration (wrong type, value not in enum, attempted write to a
// read-only setting, missing required field on a fluent builder).
type ConfigurationError struct {
	Field  string
	Reason string
}

func (e *ConfigurationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("cxone: invalid configuration for %q: %s", e.Field, e.Reason)
	}
	return fmt.Sprintf("cxone: invalid configuration: %s", e.Reason)
}

// ReportError is raised when a report request is rejected, report polling
// times out, or a report file format is unsupported.
type ReportError struct {
	ReportID string
	Reason   string
	Cause    error
}

func (e *ReportError) Error() string {
	if e.ReportID != "" {
		return fmt.Sprintf("cxone: report %s failed: %s", e.ReportID, e.Reason)
	}
	return fmt.Sprintf("cxone: report failed: %s", e.Reason)
}

func (e *ReportError) Unwrap() error { return e.Cause }

// Sentinel guarantees the catalog implements error.
var (
	_ error = (*EndpointError)(nil)
	_ error = (*AuthError)(nil)
	_ error = (*CommunicationError)(nil)
	_ error = (*ResponseError)(nil)
	_ error = (*ScanError)(nil)
	_ error = (*ConfigurationError)(nil)
	_ error = (*ReportError)(nil)
)

// Is helpers so callers can use errors.Is with sentinel checks if they wish.
// (errors.As against the concrete types remains the primary discrimination
// mechanism — these are just convenience predicates.)
var (
	// ErrAuth matches any AuthError via errors.Is.
	ErrAuth = errors.New("cxone: auth")
	// ErrCommunication matches any CommunicationError via errors.Is.
	ErrCommunication = errors.New("cxone: communication")
	// ErrResponse matches any ResponseError via errors.Is.
	ErrResponse = errors.New("cxone: response")
)

func (e *AuthError) Is(target error) bool          { return target == ErrAuth }
func (e *CommunicationError) Is(target error) bool { return target == ErrCommunication }
func (e *ResponseError) Is(target error) bool      { return target == ErrResponse }

// TriageErrorKind classifies the documented AI Triage submit/retrieve
// failures that callers commonly handle (ADR-010 fail-open policy).
type TriageErrorKind string

const (
	// TriageSourceUnavailable is HTTP 400: the source code is no longer
	// available, so triage cannot run.
	TriageSourceUnavailable TriageErrorKind = "source_unavailable"
	// TriageInsufficientCredits is HTTP 402: AI consumption credits exhausted.
	TriageInsufficientCredits TriageErrorKind = "insufficient_credits"
	// TriageForbidden is HTTP 403: feature not enabled or insufficient roles.
	TriageForbidden TriageErrorKind = "forbidden"
)

// TriageError is returned by the AI Triage endpoints for HTTP 400, 402 and
// 403. The originating *ResponseError is preserved and reachable through
// errors.As. Message is redacted before construction.
type TriageError struct {
	Kind          TriageErrorKind
	Method        string
	URL           string
	StatusCode    int
	CorrelationID string
	Message       string
	Cause         error
}

func (e *TriageError) Error() string {
	return fmt.Sprintf(
		"cxone: ai triage %s: %s %s returned status %d (correlationId=%s): %s",
		e.Kind, e.Method, e.URL, e.StatusCode, e.CorrelationID, e.Message,
	)
}

func (e *TriageError) Unwrap() error { return e.Cause }

var _ error = (*TriageError)(nil)
