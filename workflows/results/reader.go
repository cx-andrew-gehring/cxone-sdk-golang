// Package results implements the high-level Results workflow.
//
// It surfaces the cross-engine /api/results endpoint as typed helpers on a
// [Reader] handle, defaulting the mandatory "scan-id" query parameter and
// exposing the common "filter by state" path as its own method.
//
// For raw access to /api/results (e.g. to pass custom query parameters), use
// [github.com/cx-andrew-gehring/cxone-sdk-golang/cxone.Client.Advanced].Results
// directly.
package results

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/results"
	sastendpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/sastresults"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// Backend bundles the dependencies the workflow needs to call the underlying
// endpoint package.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}

// Reader is the high-level handle for the cross-engine Results API.
//
// Unlike the caching readers in workflows/presets and workflows/users, this
// reader holds no in-memory state: results are large and scan-scoped, so
// caching across scans would balloon memory for no real reuse. Construct one
// per [Backend] and reuse it across calls.
type Reader struct {
	backend *Backend
	paging  pagination.Config
}

// NewReader constructs a Reader. The reader is ready to use immediately;
// each method validates inputs and surfaces backend misconfiguration as
// *cxerrors.ConfigurationError.
func NewReader(backend *Backend) *Reader {
	return &Reader{backend: backend}
}

// WithPagination returns a copy of the reader whose raw streaming methods
// ([Reader.Stream], [Reader.StreamSAST]) use cfg (page size, offset mode,
// retries). The zero Config selects each endpoint's spec default paging
// mode; see [pagination.OffsetMode].
func (r *Reader) WithPagination(cfg pagination.Config) *Reader {
	cp := *r
	cp.paging = cfg
	return &cp
}

// Stream pages through every result of scanID via GET /api/results and
// calls fn with the raw JSON of each result (unknown fields preserved).
// Memory use is bounded by one page. It stops at the first error from fn
// (returned as-is), a page fetch failure, or ctx cancellation.
func (r *Reader) Stream(ctx context.Context, scanID string, fn func(json.RawMessage) error) error {
	return r.stream(ctx, scanID, nil, fn, endpoints.IterRaw, nil)
}

// StreamSAST is like [Reader.Stream] but uses GET /api/sast-results and
// requests the given visible-columns (e.g. "result-id", "is-ai-generated").
// An empty visibleColumns list leaves the server default.
func (r *Reader) StreamSAST(ctx context.Context, scanID string, visibleColumns []string, fn func(json.RawMessage) error) error {
	return r.stream(ctx, scanID, visibleColumns, fn, sastendpoints.IterRaw, nil)
}

// StreamWithStats is [Reader.Stream] that also returns how many items were
// delivered and the server's totalCount. The SDK ends a stream on a short
// page, so a service-side page-size cap below the configured page size ends
// it after one page; [pagination.StreamStats.Truncated] detects that. The
// stats are filled in on error too (items delivered so far).
func (r *Reader) StreamWithStats(ctx context.Context, scanID string, fn func(json.RawMessage) error) (pagination.StreamStats, error) {
	var st pagination.StreamStats
	err := r.stream(ctx, scanID, nil, fn, endpoints.IterRaw, &st)
	return st, err
}

// StreamSASTWithStats is [Reader.StreamSAST] with the stats of
// [Reader.StreamWithStats].
func (r *Reader) StreamSASTWithStats(ctx context.Context, scanID string, visibleColumns []string, fn func(json.RawMessage) error) (pagination.StreamStats, error) {
	var st pagination.StreamStats
	err := r.stream(ctx, scanID, visibleColumns, fn, sastendpoints.IterRaw, &st)
	return st, err
}

type rawIterFunc func(e *transport.Executor, baseURL string, query url.Values, cfg pagination.Config) *pagination.Iterator[json.RawMessage]

func (r *Reader) stream(ctx context.Context, scanID string, visibleColumns []string, fn func(json.RawMessage) error, mk rawIterFunc, st *pagination.StreamStats) error {
	if err := r.checkBackend(); err != nil {
		return err
	}
	if scanID == "" {
		return &cxerrors.ConfigurationError{Field: "scanID", Reason: "is required"}
	}
	if fn == nil {
		return &cxerrors.ConfigurationError{Field: "fn", Reason: "is required"}
	}
	q := url.Values{"scan-id": {scanID}}
	if len(visibleColumns) > 0 {
		q["visible-columns"] = append([]string(nil), visibleColumns...)
	}
	it := mk(r.backend.Executor, r.backend.BaseURL, q, r.paging)
	if st != nil {
		defer func() { st.Total, st.TotalKnown = it.Total() }()
	}
	for {
		item, ok, err := it.Next(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if st != nil {
			st.Delivered++
		}
		if err := fn(item); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// ListAll iterates every result page for scanID and returns the flattened
// slice. query MAY contain additional filters (severity, state, status,
// sort); "scan-id" is always set from the scanID argument and overrides any
// value in query.
//
// Beware: scans with tens of thousands of results will materialize the whole
// slice. For large scans prefer [Reader.Iter] and stream item by item.
func (r *Reader) ListAll(ctx context.Context, scanID string, query url.Values) ([]*models.ScanResult, error) {
	if err := r.checkBackend(); err != nil {
		return nil, err
	}
	if scanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "scanID", Reason: "is required"}
	}
	it := r.iter(scanID, query)
	return pagination.Collect(ctx, it)
}

// ListByState is shorthand for ListAll with a "state" filter applied. The
// CxOne state vocabulary is (verbatim): TO_VERIFY, NOT_EXPLOITABLE,
// CONFIRMED, URGENT, PROPOSED_NOT_EXPLOITABLE.
func (r *Reader) ListByState(ctx context.Context, scanID, state string) ([]*models.ScanResult, error) {
	if state == "" {
		return nil, &cxerrors.ConfigurationError{Field: "state", Reason: "is required"}
	}
	q := url.Values{}
	q.Set("state", state)
	return r.ListAll(ctx, scanID, q)
}

// Iter returns an item-yielding iterator over the results matching scanID
// plus the additional filters in query. The iterator transparently pages the
// upstream endpoint. See [pagination.Iterator] for the streaming contract.
//
// "scan-id" is set from the scanID argument and overrides any value in query.
func (r *Reader) Iter(scanID string, query url.Values) *pagination.Iterator[*models.ScanResult] {
	return r.iter(scanID, query)
}

// iter is the shared constructor used by both ListAll and Iter so the two
// paths cannot diverge on the "scan-id" handling.
func (r *Reader) iter(scanID string, query url.Values) *pagination.Iterator[*models.ScanResult] {
	q := url.Values{}
	for k, vs := range query {
		cp := make([]string, len(vs))
		copy(cp, vs)
		q[k] = cp
	}
	q.Set("scan-id", scanID)
	return endpoints.Iter(r.backend.Executor, r.backend.BaseURL, q, pagination.Config{})
}

// checkBackend asserts the workflow has a usable backend; surfaces the error
// before any other validation so the misconfiguration is unambiguous.
func (r *Reader) checkBackend() error {
	if r.backend == nil || r.backend.Executor == nil {
		return &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	return nil
}
