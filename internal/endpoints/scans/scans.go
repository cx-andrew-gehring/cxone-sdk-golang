// Package scans implements the Checkmarx One Scans API (low-level layer).
//
// See package projects for the design rationale; this package follows the
// same conventions: one exported function per endpoint, every call goes
// through the supplied [*transport.Executor], typed in/out, no global state.
//
// Most users should consume the high-level workflow layer (cxone/workflows/scans)
// rather than this package directly. Reach this package via cxone.Client.Advanced().
package scans

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// Path is the relative path appended to the API base URL for the scans
// resource.
const Path = "api/scans"

// Create starts a new scan. POST /api/scans → 201 Created.
func Create(ctx context.Context, e *transport.Executor, baseURL string, in *models.Scan) (*models.ScanResponseModel, error) {
	var out models.ScanResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, in, []int{http.StatusCreated}, &out); err != nil {
		// CxOne answers a Fusion scan with an unknown LLM as 400 code 209.
		var respErr *cxerrors.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == http.StatusBadRequest &&
			respErr.ErrorCode == cxerrors.InvalidLLMErrorCode {
			return nil, &cxerrors.InvalidLLMError{
				Message: respErr.ServerMessage, CorrelationID: respErr.CorrelationID, Cause: respErr,
			}
		}
		return nil, err
	}
	return &out, nil
}

// List returns a paginated list of scans. GET /api/scans → 200.
//
// Common query parameters: project-id, branch, statuses, tags-keys,
// tags-values, from-date, to-date, limit, offset.
func List(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (*models.ScansCollectionResponseModel, error) {
	var out models.ScansCollectionResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Iter returns an item-yielding iterator over the scans matching query.
// See [github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/projects.Iter]
// for the offset-handling rationale; this function follows the same pattern.
func Iter(e *transport.Executor, baseURL string, query url.Values, cfg pagination.Config) *pagination.Iterator[models.ScanResponseModel] {
	cfg.OffsetIsByCount = true
	if cfg.PageSize == 0 {
		cfg.PageSize = 100
	}
	return pagination.New(cfg, func(ctx context.Context, offset, limit int) ([]models.ScanResponseModel, error) {
		q := cloneValues(query)
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(limit))
		coll, err := List(ctx, e, baseURL, q)
		if err != nil {
			return nil, err
		}
		return coll.Scans, nil
	})
}

// cloneValues returns a shallow copy of v.
func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v)+2)
	for k, vs := range v {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

// Get fetches a single scan by id. GET /api/scans/{id} → 200.
func Get(ctx context.Context, e *transport.Executor, baseURL, scanID string) (*models.ScanResponseModel, error) {
	var out models.ScanResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+scanID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWorkflow returns the timeline of internal workflow events for a scan.
// GET /api/scans/{id}/workflow → 200.
func GetWorkflow(ctx context.Context, e *transport.Executor, baseURL, scanID string) ([]models.ScanTaskResponseModel, error) {
	var out []models.ScanTaskResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+scanID+"/workflow"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Cancel asks the platform to stop a running scan.
// PATCH /api/scans/{id} with body {"status":"Canceled"} → 204.
func Cancel(ctx context.Context, e *transport.Executor, baseURL, scanID string) error {
	body := models.CancelScanModel{Status: models.ScanCanceled}
	return transport.DoJSON(ctx, e, http.MethodPatch,
		transport.JoinURL(baseURL, Path+"/"+scanID),
		nil, body, []int{http.StatusNoContent}, nil)
}

// Delete removes a scan record. DELETE /api/scans/{id} → 204.
func Delete(ctx context.Context, e *transport.Executor, baseURL, scanID string) error {
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path+"/"+scanID),
		nil, nil, []int{http.StatusNoContent}, nil)
}

// Tags returns the map of tag keys to known values across all scans.
// GET /api/scans/tags → 200.
func Tags(ctx context.Context, e *transport.Executor, baseURL string) (map[string][]string, error) {
	out := map[string][]string{}
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/tags"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
