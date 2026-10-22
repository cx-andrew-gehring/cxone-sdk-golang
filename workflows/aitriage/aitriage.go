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

// Failed reports whether the triage job failed: jobStatus FAILED, or a
// detail whose triageStatus is FAILED.
func (r *ResultInspector) Failed() bool {
	if r.env == nil {
		return false
	}
	if r.env.Detail != nil {
		return r.env.Detail.TriageStatus == "FAILED"
	}
	return r.env.JobStatus == "FAILED"
}

// Done reports whether polling can stop: the job failed, or the full detail
// has been returned (and is not itself reporting IN_PROGRESS).
func (r *ResultInspector) Done() bool {
	if r.env == nil {
		return false
	}
	if r.env.Detail != nil {
		return r.env.Detail.TriageStatus != "IN_PROGRESS"
	}
	return r.env.JobStatus == "FAILED"
}

// Detail returns the completed result, or nil while the job is in progress
// or failed.
func (r *ResultInspector) Detail() *models.TriageResultDetail {
	if r.env == nil {
		return nil
	}
	return r.env.Detail
}
