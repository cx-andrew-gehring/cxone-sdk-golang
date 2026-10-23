// Package sastresults implements GET /api/sast-results (low-level layer),
// the SAST-only results endpoint that supports "visible-columns" (including
// "is-ai-generated").
package sastresults

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// Path is the relative path to the SAST results endpoint.
const Path = "api/sast-results"

type rawPage struct {
	Results []json.RawMessage `json:"results"`
	// TotalCount is the spec's "totalCount" (sastResults.yaml); nil when
	// the server omits it.
	TotalCount *int `json:"totalCount"`
}

// IterRaw returns an iterator over the raw JSON of each SAST result.
// query MUST contain "scan-id" and may contain "visible-columns"
// (repeated values), filters, and sort.
//
// Paging: sastResults.yaml documents "offset" as "the number of results to
// skip" (unlike /api/results, which counts pages), so the default
// (cfg.OffsetMode == [pagination.OffsetModeDefault]) is item-count paging.
// Spike S1 has not yet confirmed live behaviour; override via
// cfg.OffsetMode. The spec default page size is 20 and max 10000; we default
// to 1000. Iteration ends on a short page, once the response's "totalCount"
// has been consumed, or with [pagination.ErrPaginationStalled] if the server
// repeats a page.
func IterRaw(e *transport.Executor, baseURL string, query url.Values, cfg pagination.Config) *pagination.Iterator[json.RawMessage] {
	if cfg.OffsetMode == pagination.OffsetModeDefault {
		cfg.OffsetMode = pagination.OffsetModeItems // spec default
	}
	if cfg.PageSize == 0 {
		cfg.PageSize = 1000
	}
	var totalN int
	var totalOK bool
	opts := pagination.Options[json.RawMessage]{
		Total: func() (int, bool) { return totalN, totalOK },
		Key:   func(m json.RawMessage) string { return string(m) },
	}
	return pagination.NewWithOptions(cfg, func(ctx context.Context, offset, limit int) ([]json.RawMessage, error) {
		q := make(url.Values, len(query)+2)
		for k, vs := range query {
			q[k] = append([]string(nil), vs...)
		}
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(limit))
		var out rawPage
		if err := transport.DoJSON(ctx, e, http.MethodGet,
			transport.JoinURL(baseURL, Path),
			q, nil, []int{http.StatusOK}, &out); err != nil {
			return nil, err
		}
		totalN, totalOK = 0, false
		if out.TotalCount != nil {
			totalN, totalOK = *out.TotalCount, true
		}
		return out.Results, nil
	}, opts)
}
