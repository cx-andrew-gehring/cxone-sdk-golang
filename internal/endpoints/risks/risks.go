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

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/redact"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
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

// MaxPages is the hard cap on pages fetched by one [Iter]. At the default page
// size of 200 it allows 2,000,000 risks, far beyond any real project; reaching
// it means the server is misbehaving and Iter fails with a ResponseError rather
// than looping forever.
const MaxPages = 10000

// Iter returns an item-yielding iterator over every risk matching query
// (which must include projectId). Offset/limit are managed by the iterator;
// the default page size is 200 (the service maximum) and larger values are
// clamped to 200.
//
// Iteration stops, without an extra empty-page request, as soon as
// offset+len(page) reaches metaData.filteredResults (falling back to
// totalResults when filteredResults is 0 on a non-empty page) or a page is
// shorter than the requested limit. A stall guard also stops iteration when a
// page contains no risk ids not already seen (a server ignoring offset), and
// MaxPages bounds the total number of requests.
func Iter(e *transport.Executor, baseURL string, query url.Values, cfg pagination.Config) *pagination.Iterator[models.Risk] {
	cfg.OffsetIsByCount = true
	if cfg.PageSize <= 0 || cfg.PageSize > 200 {
		cfg.PageSize = 200
	}
	if cfg.Retryable == nil {
		// Client errors (4xx) will not heal on retry.
		cfg.Retryable = func(err error) bool {
			var re *cxerrors.ResponseError
			return !errors.As(err, &re) || re.StatusCode >= 500
		}
	}
	var (
		done  bool
		pages int
		seen  = map[string]struct{}{}
	)
	return pagination.New(cfg, func(ctx context.Context, offset, limit int) ([]models.Risk, error) {
		if done {
			return nil, nil
		}
		if pages >= MaxPages {
			return nil, &cxerrors.ResponseError{
				Method: http.MethodGet, URL: redact.URL(transport.JoinURL(baseURL, RisksPath)),
				StatusCode: http.StatusOK,
				Reason:     "risks listing exceeded max pages (" + strconv.Itoa(MaxPages) + "); server is not honouring offset/limit",
			}
		}
		pages++
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
		n := len(page.Risks)
		total := page.MetaData.FilteredResults
		if total == 0 && n > 0 {
			total = page.MetaData.TotalResults
		}
		if n == 0 || n < limit || (total > 0 && offset+n >= total) {
			done = true
		}
		// Stall guard: keep only risks not seen before; a page of nothing new
		// means the server is repeating itself.
		fresh := page.Risks[:0:0]
		for _, r := range page.Risks {
			if r.ID == "" {
				fresh = append(fresh, r)
				continue
			}
			if _, dup := seen[r.ID]; dup {
				continue
			}
			seen[r.ID] = struct{}{}
			fresh = append(fresh, r)
		}
		if len(fresh) == 0 {
			done = true
		}
		return fresh, nil
	})
}
