// Package credits implements read access to the (undocumented) Checkmarx One
// credits endpoints: consumption per user and the credit rate per action.
package credits

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// Paths relative to the API host root.
const (
	ConsumptionPath = "api/v1/credits/consumption"
	ActionRatesPath = "api/v1/credits/action-rates"
)

const (
	defaultPageParam  = "page"
	defaultLimitParam = "limit"
)

func values(q models.CreditConsumptionQuery, page int) url.Values {
	pp, lp := q.PageParam, q.LimitParam
	if pp == "" {
		pp = defaultPageParam
	}
	if lp == "" {
		lp = defaultLimitParam
	}
	v := url.Values{}
	if page > 0 {
		v.Set(pp, strconv.Itoa(page))
	}
	if q.Limit > 0 {
		v.Set(lp, strconv.Itoa(q.Limit))
	}
	return v
}

// Consumption fetches one page. GET /api/v1/credits/consumption -> 200.
func Consumption(ctx context.Context, e *transport.Executor, baseURL string, q models.CreditConsumptionQuery) (*models.CreditConsumptionPage, error) {
	return fetch(ctx, e, baseURL, values(q, q.Page))
}

func fetch(ctx context.Context, e *transport.Executor, baseURL string, v url.Values) (*models.CreditConsumptionPage, error) {
	var out models.CreditConsumptionPage
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, ConsumptionPath),
		v, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConsumptionAll walks every page, driven by the response's totalPages, and
// returns all items. On any page failure (after the funnel's own retries) it
// returns (nil, err): never a partial result. It also fails if a page's
// currentPage differs from the page requested, or if the page count is not
// stable or exceeds pagination.DefaultMaxPages.
func ConsumptionAll(ctx context.Context, e *transport.Executor, baseURL string, q models.CreditConsumptionQuery) ([]models.CreditConsumptionItem, error) {
	first := q.FirstPage
	if first <= 0 {
		first = 1
	}
	var all []models.CreditConsumptionItem
	for page, n := first, 0; ; page, n = page+1, n+1 {
		if n >= pagination.DefaultMaxPages {
			return nil, pagination.ErrMaxPagesExceeded
		}
		res, err := fetch(ctx, e, baseURL, values(q, page))
		if err != nil {
			return nil, err
		}
		if res.CurrentPage != page {
			return nil, fmt.Errorf("credits consumption: requested page %d but service returned currentPage %d (wrong paging parameter or first-page base?)", page, res.CurrentPage)
		}
		all = append(all, res.Items...)
		// totalPages counts pages from the first one; the last page number is
		// first+totalPages-1.
		if res.TotalPages <= 0 || len(res.Items) == 0 || page >= first+res.TotalPages-1 {
			return all, nil
		}
	}
}

// ActionRates returns the raw body of GET /api/v1/credits/action-rates. The
// JSON shape is not yet confirmed, so it is not decoded.
func ActionRates(ctx context.Context, e *transport.Executor, baseURL string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, ActionRatesPath),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
