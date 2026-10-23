// Package aitriage implements the high-level AI Triage workflow: submit a
// triage request for a scan's findings and inspect per-group results.
//
// Per ADR-010 callers that must fail open on missing credits or feature
// access should test the returned error with errors.As against
// *cxerrors.TriageError (kinds: TriageInsufficientCredits, TriageForbidden,
// TriageSourceUnavailable).
package aitriage

import (
	"context"
	"errors"
	"net/http"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	endpoints "github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/aitriage"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
)

// Backend bundles the dependencies the workflow needs.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}

func (b *Backend) check() error {
	if b == nil || b.Executor == nil {
		return &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	return nil
}

// Submit requests AI triage of the given buckets for scanID. A response with
// Published=false (identical job already exists) is returned as success.
func Submit(ctx context.Context, b *Backend, scanID string, buckets ...models.TriageBucket) (*models.TriageAccepted, error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	if scanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "scanID", Reason: "is required"}
	}
	if len(buckets) == 0 {
		return nil, &cxerrors.ConfigurationError{Field: "buckets", Reason: "at least one bucket is required"}
	}
	return endpoints.Submit(ctx, b.Executor, b.BaseURL, &models.TriageRequest{ScanID: scanID, Buckets: buckets})
}

// GetResult fetches the triage state of one vulnerability group and wraps it
// in a [ResultInspector].
func GetResult(ctx context.Context, b *Backend, projectID, groupID string) (*ResultInspector, error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	if groupID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "groupID", Reason: "is required"}
	}
	env, err := endpoints.GetResult(ctx, b.Executor, b.BaseURL, projectID, groupID)
	if err != nil {
		return nil, err
	}
	return &ResultInspector{env: env}, nil
}

// ResultInspector is a typed view over a triage result envelope.
type ResultInspector struct {
	env *models.TriageResultEnvelope
}

// Envelope returns the raw decoded response.
func (r *ResultInspector) Envelope() *models.TriageResultEnvelope { return r.env }

// Triage status values from the TriageStatus enum in AI-Triage.yaml. They
// appear as jobStatus (in-progress shape) or triageStatus (detail shape).
const (
	StatusNotTriaged             = "NOT_TRIAGED"
	StatusInProgress             = "IN_PROGRESS"
	StatusFailed                 = "FAILED"
	StatusVulnerable             = "VULNERABLE"
	StatusProposedNotExploitable = "PROPOSED_NOT_EXPLOITABLE"
	StatusUncertain              = "UNCERTAIN"
	StatusRiskAccepted           = "RISK_ACCEPTED"
	// StatusConfirmed is not in the spec enum but the live service returns
	// it as a terminal status (spike S1 policy run: IN_PROGRESS -> CONFIRMED,
	// the finding's state went TO_VERIFY -> CONFIRMED).
	StatusConfirmed = "CONFIRMED"
)

// Status returns the effective status string: the detail's triageStatus when
// the detail shape was returned, otherwise jobStatus.
func (r *ResultInspector) Status() string {
	if r.env == nil {
		return ""
	}
	if r.env.Detail != nil {
		return r.env.Detail.TriageStatus
	}
	return r.env.JobStatus
}

// Done reports whether polling can stop: the status is terminal, i.e.
// VULNERABLE, PROPOSED_NOT_EXPLOITABLE, UNCERTAIN, RISK_ACCEPTED, CONFIRMED
// or FAILED. NOT_TRIAGED, IN_PROGRESS and unrecognised statuses are not done.
func (r *ResultInspector) Done() bool {
	switch r.Status() {
	case StatusVulnerable, StatusProposedNotExploitable, StatusUncertain, StatusRiskAccepted, StatusConfirmed, StatusFailed:
		return true
	default:
		return false
	}
}

// RawDetail returns the detail-shape body exactly as received, nil for the
// jobStatus shape. Unlike re-encoding [ResultInspector.Detail], it keeps
// unknown fields and false/empty values.
func (r *ResultInspector) RawDetail() []byte {
	if r.env == nil || r.env.RawDetail == nil {
		return nil
	}
	return append([]byte(nil), r.env.RawDetail...)
}

// Failed reports whether the triage job failed (status FAILED, in either
// shape). A failed result is also Done.
func (r *ResultInspector) Failed() bool { return r.Status() == StatusFailed }

// Unknown reports whether the status is non-empty but not in the spec enum
// (for example a value added by a newer service). Unknown results are neither
// Done nor Failed; callers should stop polling or log rather than spin.
func (r *ResultInspector) Unknown() bool {
	switch r.Status() {
	case "", StatusNotTriaged, StatusInProgress, StatusFailed, StatusVulnerable,
		StatusProposedNotExploitable, StatusUncertain, StatusRiskAccepted, StatusConfirmed:
		return false
	default:
		return true
	}
}

// Detail returns the detail payload whenever the server returned the detail
// shape, whatever its status (including IN_PROGRESS or FAILED, where the
// analysis fields may be empty); it is nil for the jobStatus-only shape.
// Check Done and Failed before trusting the analysis fields.
func (r *ResultInspector) Detail() *models.TriageResultDetail {
	if r.env == nil {
		return nil
	}
	return r.env.Detail
}

// IsNotFound reports whether err (from GetResult) is an HTTP 404: no triage
// exists yet for that project/group.
func IsNotFound(err error) bool {
	var re *cxerrors.ResponseError
	return errors.As(err, &re) && re.StatusCode == http.StatusNotFound
}
