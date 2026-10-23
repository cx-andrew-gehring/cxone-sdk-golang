// Package predicates implements the Checkmarx One result triage / predicates
// endpoints across SAST, KICS, SCA, and SCS engines.
//
// Each engine has its own predicate path because the request shapes diverge
// (SCA uses package vulnerabilities; SCS uses the micro-engines proxy).
package predicates

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path constants for the per-engine predicate endpoints.
const (
	SastPath        = "api/sast-results-predicates"
	KicsPath        = "api/kics-results-predicates"
	ScaPath         = "api/sca/management-of-risk/package-vulnerabilities"
	ScsReadPath     = "api/micro-engines/read/predicates"
	ScsWritePath    = "api/micro-engines/write/predicates"
	CustomStatesPath = "api/custom-states"
)

// GetSAST returns the predicate history for a SAST similarity id, optionally
// filtered to a list of project ids.
// GET /api/sast-results-predicates/{similarityId}?project-ids={ids} → 200.
func GetSAST(ctx context.Context, e *transport.Executor, baseURL, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return getPredicates(ctx, e, baseURL, SastPath+"/"+similarityID, projectIDs)
}

// GetKICS is the KICS counterpart of [GetSAST].
func GetKICS(ctx context.Context, e *transport.Executor, baseURL, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return getPredicates(ctx, e, baseURL, KicsPath+"/"+similarityID, projectIDs)
}

// GetSCS is the SCS counterpart of [GetSAST].
func GetSCS(ctx context.Context, e *transport.Executor, baseURL, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return getPredicates(ctx, e, baseURL, ScsReadPath+"/"+similarityID, projectIDs)
}

func getPredicates(ctx context.Context, e *transport.Executor, baseURL, path string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	q := url.Values{}
	for _, id := range projectIDs {
		q.Add("project-ids", id)
	}
	var out models.PredicatesCollectionResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, path),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSAST submits a batch of state-change predicates for SAST results.
// POST /api/sast-results-predicates → 200/201.
func UpdateSAST(ctx context.Context, e *transport.Executor, baseURL string, in []models.PredicateRequest) error {
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, SastPath),
		nil, in, []int{http.StatusOK, http.StatusCreated, http.StatusNotModified}, nil)
}

// UpdateKICS is the KICS counterpart of [UpdateSAST].
func UpdateKICS(ctx context.Context, e *transport.Executor, baseURL string, in []models.PredicateRequest) error {
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, KicsPath),
		nil, in, []int{http.StatusOK, http.StatusCreated, http.StatusNotModified}, nil)
}

// UpdateSCS submits a single SCS predicate change.
// POST /api/micro-engines/write/predicates → 200/201.
func UpdateSCS(ctx context.Context, e *transport.Executor, baseURL string, in *models.PredicateRequest) error {
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, ScsWritePath),
		nil, in, []int{http.StatusOK, http.StatusCreated, http.StatusNotModified}, nil)
}

// UpdateSCA submits a SCA package-vulnerability predicate.
// POST /api/sca/management-of-risk/package-vulnerabilities → 200/201.
func UpdateSCA(ctx context.Context, e *transport.Executor, baseURL string, in *models.ScaPredicateRequest) error {
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, ScaPath),
		nil, in, []int{http.StatusOK, http.StatusCreated, http.StatusNotModified}, nil)
}

// ListCustomStates returns all custom triage states defined for the tenant.
// GET /api/custom-states?include-deleted=true|false → 200.
func ListCustomStates(ctx context.Context, e *transport.Executor, baseURL string, includeDeleted bool) ([]models.CustomState, error) {
	q := url.Values{}
	if includeDeleted {
		q.Set("include-deleted", "true")
	}
	var out []models.CustomState
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, CustomStatesPath),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateCustomState creates a new tenant custom triage state.
// POST /api/custom-states → 201.
func CreateCustomState(ctx context.Context, e *transport.Executor, baseURL, name string) (*models.CustomState, error) {
	if name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "name", Reason: "is required"}
	}
	var out models.CustomState
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, CustomStatesPath),
		nil, models.CustomStateCreateRequest{Name: name},
		[]int{http.StatusCreated, http.StatusOK}, &out); err != nil {
		return nil, err
	}
	out.IsAllowed = true
	return &out, nil
}

// DeleteCustomState removes a custom triage state by id.
// DELETE /api/custom-states/{id} → 204.
func DeleteCustomState(ctx context.Context, e *transport.Executor, baseURL string, stateID int) error {
	if stateID <= 0 {
		return &cxerrors.ConfigurationError{Field: "stateID", Reason: "must be > 0"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, CustomStatesPath+"/"+strconv.Itoa(stateID)),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// GetSASTLatest returns the latest SAST predicate per project for a given
// similarity id.
// GET /api/sast-results-predicates/{similarityId}/latest?project-ids={ids} → 200.
func GetSASTLatest(ctx context.Context, e *transport.Executor, baseURL, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	if similarityID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "similarityID", Reason: "is required"}
	}
	q := url.Values{}
	for _, id := range projectIDs {
		q.Add("project-ids", id)
	}
	var out models.PredicatesCollectionResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, SastPath+"/"+similarityID+"/latest"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSASTWithScan is like [GetSAST] but also filters the history to a single
// scan id. Useful when the caller cares about predicates captured *during*
// that specific scan rather than the full timeline.
func GetSASTWithScan(ctx context.Context, e *transport.Executor, baseURL, similarityID, scanID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	if similarityID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "similarityID", Reason: "is required"}
	}
	q := url.Values{}
	for _, id := range projectIDs {
		q.Add("project-ids", id)
	}
	if scanID != "" {
		q.Set("scan-id", scanID)
	}
	var out models.PredicatesCollectionResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, SastPath+"/"+similarityID),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetChangeHistory returns the full results-change timeline for an entity.
// entityType is one of "projectID", "scanID", or "similarityID"; entityID is
// the value.
//
// GET /api/sast-results-predicates/changelog → 200.
func GetChangeHistory(ctx context.Context, e *transport.Executor, baseURL, entityType, entityID string, limit, offset int) (*models.ResultsChangeHistoryResponse, error) {
	if entityType == "" || entityID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "entityType/entityID", Reason: "both are required"}
	}
	q := url.Values{}
	q.Set("entity-type", entityType)
	q.Set("entity-id", entityID)
	q.Set("history", "true")
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out models.ResultsChangeHistoryResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, SastPath+"/changelog"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
