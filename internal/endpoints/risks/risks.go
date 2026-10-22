// Package risks implements the Checkmarx One Risk Management and API
// Security risks-overview endpoints.
//
// These two endpoints come from different services on the platform but are
// closely related — both surface a "risk view" of an existing scan — so
// they share a single SDK package.
package risks

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
)

// Path constants for the risk endpoints.
const (
	// RiskManagementPathTemplate carries the {projectId} placeholder. The
	// scan id is supplied as a query parameter.
	RiskManagementPathTemplate = "api/risk-management/projects/%s/results"

	// APISecOverviewPathTemplate carries the {scanId} placeholder.
	APISecOverviewPathTemplate = "api/apisec/static/api/scan/%s/risks-overview"

	// APISecRisksPath is the paginated risks listing endpoint.
	APISecRisksPath = "api/apisec/static/api/risks"
)

// GetRiskManagement returns the ASPM risk-management view of a project's
// results for a given scan.
// GET /api/risk-management/projects/{projectId}/results?scanID={id} → 200.
func GetRiskManagement(ctx context.Context, e *transport.Executor, baseURL, projectID, scanID string) (*models.ASPMResult, error) {
	path := "api/risk-management/projects/" + projectID + "/results"
	q := url.Values{"scanID": {scanID}}
	var out models.ASPMResult
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, path),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAPISecOverview returns the API security risks overview for a scan.
// GET /api/apisec/static/api/scan/{scanId}/risks-overview → 200.
func GetAPISecOverview(ctx context.Context, e *transport.Executor, baseURL, scanID string) (*models.APISecResult, error) {
	path := "api/apisec/static/api/scan/" + scanID + "/risks-overview"
	var out models.APISecResult
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, path),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAPISecRisks returns paginated API security risk entries.
// GET /api/apisec/static/api/risks?page&per_page&filtering&state&status&severity → 200.
func ListAPISecRisks(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (*models.APISecPaginatedResult, error) {
	var out models.APISecPaginatedResult
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, APISecRisksPath),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RisksPath is the project risks listing endpoint (RiskOrchestration.yaml).
const RisksPath = "api/risks"

// ListRisks returns one page of risks for a project.
// GET /api/risks?projectId&engine&limit&offset → 200. query may carry
// repeated "engine" values; limit is capped at 200 by the service.
func ListRisks(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (*models.RisksPage, error) {
	var out models.RisksPage
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, RisksPath),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Iter returns an item-yielding iterator over every risk matching query
// (which must include projectId). Offset/limit are managed by the iterator;
// the default page size is 100 and is clamped to the service maximum of 200.
func Iter(e *transport.Executor, baseURL string, query url.Values, cfg pagination.Config) *pagination.Iterator[models.Risk] {
	cfg.OffsetIsByCount = true
	if cfg.PageSize == 0 {
		cfg.PageSize = 100
	}
	if cfg.PageSize > 200 {
		cfg.PageSize = 200
	}
	if cfg.Retryable == nil {
		// Client errors (4xx) will not heal on retry.
		cfg.Retryable = func(err error) bool {
			var re *cxerrors.ResponseError
			return !errors.As(err, &re) || re.StatusCode >= 500
		}
	}
	return pagination.New(cfg, func(ctx context.Context, offset, limit int) ([]models.Risk, error) {
		q := make(url.Values, len(query)+2)
		for k, vs := range query {
			q[k] = append([]string(nil), vs...)
		}
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(limit))
		page, err := ListRisks(ctx, e, baseURL, q)
		if err != nil {
			return nil, err
		}
		return page.Risks, nil
	})
}
