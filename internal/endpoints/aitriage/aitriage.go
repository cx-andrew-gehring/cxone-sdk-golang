// Package aitriage implements the Checkmarx One AI Triage endpoints
// (AI-Triage.yaml): submit a triage request and fetch a group's result.
package aitriage

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/redact"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
)

// Path constants.
const (
	SubmitPath    = "api/ai-triage/triage"
	ResultPathFmt = "api/ai-triage/triage/"
)

// Submit initiates AI triage for the buckets in in.
// POST /api/ai-triage/triage → 202. A response with published=false (an
// identical job already exists) is a success. HTTP 400, 402 and 403 are
// returned as *cxerrors.TriageError.
func Submit(ctx context.Context, e *transport.Executor, baseURL string, in *models.TriageRequest) (*models.TriageAccepted, error) {
	var out models.TriageAccepted
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, SubmitPath),
		nil, in, []int{http.StatusAccepted}, &out); err != nil {
		return nil, mapError(err, true)
	}
	return &out, nil
}

// GetResult fetches the triage result for a vulnerability group.
// GET /api/ai-triage/triage/{project_id}/{group_id} → 200 (oneOf in-progress
// status or detail). groupID is path-escaped (SCA group ids contain "#").
// HTTP 403 is returned as *cxerrors.TriageError.
func GetResult(ctx context.Context, e *transport.Executor, baseURL, projectID, groupID string) (*models.TriageResultEnvelope, error) {
	path := ResultPathFmt + url.PathEscape(projectID) + "/" + url.PathEscape(groupID)
	var out models.TriageResultEnvelope
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, path),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, mapError(err, false)
	}
	return &out, nil
}

// mapError converts documented HTTP failures into *cxerrors.TriageError.
// 400 is only documented for submit.
func mapError(err error, submit bool) error {
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) {
		return err
	}
	var kind cxerrors.TriageErrorKind
	switch {
	case re.StatusCode == http.StatusBadRequest && submit:
		kind = cxerrors.TriageSourceUnavailable
	case re.StatusCode == http.StatusPaymentRequired && submit:
		kind = cxerrors.TriageInsufficientCredits
	case re.StatusCode == http.StatusForbidden:
		kind = cxerrors.TriageForbidden
	default:
		return err
	}
	return &cxerrors.TriageError{
		Kind: kind, Method: re.Method, URL: redact.URL(re.URL), StatusCode: re.StatusCode,
		CorrelationID: re.CorrelationID, Message: redact.String(re.Reason), Cause: err,
	}
}
