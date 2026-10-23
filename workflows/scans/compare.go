package scans

import (
	"context"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/results"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// CompareOption configures one call to [Backend.Compare].
type CompareOption func(*compareOptions)

type compareOptions struct {
	identityKey models.IdentityKey
	severities  map[string]struct{}
	engines     map[string]struct{}
	progress    func(stage string, done, total int)
}

func (o *compareOptions) wantSeverity(s string) bool {
	if o.severities == nil {
		return true
	}
	_, ok := o.severities[s]
	return ok
}

func (o *compareOptions) wantEngine(t string) bool {
	if o.engines == nil {
		return true
	}
	_, ok := o.engines[t]
	return ok
}

// WithIdentityKey selects which result field identifies a finding across the
// two scans. Defaults to [models.IdentityKeySimilarityID], which matches the
// behavior of the legacy CxSAST SOAP comparison endpoint.
func WithIdentityKey(k models.IdentityKey) CompareOption {
	return func(o *compareOptions) { o.identityKey = k }
}

// WithSeverities limits the comparison to the named severities (verbatim,
// case-sensitive). When unset, every severity present in either scan is
// included.
func WithSeverities(severities ...string) CompareOption {
	return func(o *compareOptions) {
		if len(severities) == 0 {
			o.severities = nil
			return
		}
		o.severities = make(map[string]struct{}, len(severities))
		for _, s := range severities {
			o.severities[s] = struct{}{}
		}
	}
}

// WithEngines limits the comparison to findings produced by the named engines
// (matched against [models.ScanResult.Type]). When unset, every engine type
// is included.
func WithEngines(types ...string) CompareOption {
	return func(o *compareOptions) {
		if len(types) == 0 {
			o.engines = nil
			return
		}
		o.engines = make(map[string]struct{}, len(types))
		for _, t := range types {
			o.engines[t] = struct{}{}
		}
	}
}

// WithProgress installs a callback the SDK invokes between stages of the
// comparison: "fetch-old", "fetch-new", "diff", "not-exploitable" — each with
// done==1, total==4 at completion. The callback runs synchronously; do not
// block on it. Optional.
func WithProgress(cb func(stage string, done, total int)) CompareOption {
	return func(o *compareOptions) { o.progress = cb }
}

// Compare diffs the results of two scans and returns counts of new, resolved,
// and recurrent findings bucketed by severity. The not-exploitable list is
// extracted from the new scan and grouped by query name.
//
// This is the high-level replacement for the legacy CxSAST SOAP
// GetScanCompareSummary endpoint: it materializes the typed
// [models.ScanComparison] consumers need without forcing them to fetch and
// diff results themselves.
//
// The old and new scans are fetched concurrently. Cancellation of ctx
// propagates to both fetches.
func (b *Backend) Compare(ctx context.Context, oldScanID, newScanID string, opts ...CompareOption) (*models.ScanComparison, error) {
	if b == nil || b.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if oldScanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "oldScanID", Reason: "is required"}
	}
	if newScanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "newScanID", Reason: "is required"}
	}

	cfg := compareOptions{identityKey: models.IdentityKeySimilarityID}
	for _, opt := range opts {
		opt(&cfg)
	}
	switch cfg.identityKey {
	case models.IdentityKeySimilarityID, models.IdentityKeyResultHash:
		// ok
	default:
		return nil, &cxerrors.ConfigurationError{Field: "IdentityKey", Reason: "must be similarityId or resultHash"}
	}

	old, newer, err := fetchScansConcurrent(ctx, b, oldScanID, newScanID, cfg.progress)
	if err != nil {
		return nil, err
	}
	if cfg.progress != nil {
		cfg.progress("diff", 3, 4)
	}

	cmp := diffResults(oldScanID, newScanID, old, newer, &cfg)
	cmp.NotExploitable = groupNotExploitable(newer, &cfg)
	if cfg.progress != nil {
		cfg.progress("not-exploitable", 4, 4)
	}
	return cmp, nil
}

// fetchScansConcurrent fetches both scans' results in parallel. Mirrors the
// raw-goroutines + first-error pattern used elsewhere in the workflow layer
// (see workflows/presets/reader.go) — avoids pulling errgroup into the
// dependency graph for what is, in this package, a two-way fan-out.
func fetchScansConcurrent(ctx context.Context, b *Backend, oldID, newID string, progress func(string, int, int)) ([]*models.ScanResult, []*models.ScanResult, error) {
	gctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg              sync.WaitGroup
		firstErr        error
		firstErrMu      sync.Mutex
		oldRes, newRes  []*models.ScanResult
	)
	setFirstErr := func(err error) {
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		firstErrMu.Unlock()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		rs, err := fetchAllResults(gctx, b, oldID)
		if err != nil {
			setFirstErr(err)
			return
		}
		oldRes = rs
		if progress != nil {
			progress("fetch-old", 1, 4)
		}
	}()
	go func() {
		defer wg.Done()
		rs, err := fetchAllResults(gctx, b, newID)
		if err != nil {
			setFirstErr(err)
			return
		}
		newRes = rs
		if progress != nil {
			progress("fetch-new", 2, 4)
		}
	}()
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	return oldRes, newRes, nil
}

// fetchAllResults walks /api/results for scanID and returns every page's
// items as a flat slice.
func fetchAllResults(ctx context.Context, b *Backend, scanID string) ([]*models.ScanResult, error) {
	q := url.Values{}
	q.Set("scan-id", scanID)
	it := endpoints.Iter(b.Executor, b.BaseURL, q, pagination.Config{})
	return pagination.Collect(ctx, it)
}

// diffResults bucketizes the differences between old and new by severity.
// Identity is the key that decides whether two findings are the same.
func diffResults(oldScanID, newScanID string, oldRes, newRes []*models.ScanResult, cfg *compareOptions) *models.ScanComparison {
	idOf := identityFn(cfg.identityKey)

	oldByID := make(map[string]*models.ScanResult, len(oldRes))
	for _, r := range oldRes {
		if !cfg.wantSeverity(r.Severity) || !cfg.wantEngine(r.Type) {
			continue
		}
		oldByID[idOf(r)] = r
	}
	newByID := make(map[string]*models.ScanResult, len(newRes))
	for _, r := range newRes {
		if !cfg.wantSeverity(r.Severity) || !cfg.wantEngine(r.Type) {
			continue
		}
		newByID[idOf(r)] = r
	}

	out := &models.ScanComparison{
		OldScanID:   oldScanID,
		NewScanID:   newScanID,
		GeneratedAt: time.Now().UTC(),
		Identity:    string(cfg.identityKey),
		BySeverity:  map[string]models.ScanComparisonBucket{},
	}

	// New & recurrent: walk new
	for id, r := range newByID {
		bkt := out.BySeverity[r.Severity]
		if _, present := oldByID[id]; present {
			bkt.Recurrent++
		} else {
			bkt.New++
		}
		out.BySeverity[r.Severity] = bkt
	}
	// Resolved: walk old, count entries missing from new
	for id, r := range oldByID {
		if _, present := newByID[id]; present {
			continue
		}
		bkt := out.BySeverity[r.Severity]
		bkt.Resolved++
		out.BySeverity[r.Severity] = bkt
	}

	for _, b := range out.BySeverity {
		out.Totals.New += b.New
		out.Totals.Resolved += b.Resolved
		out.Totals.Recurrent += b.Recurrent
	}
	return out
}

// groupNotExploitable extracts findings in the new scan whose State is the
// not-exploitable triage state, groups them by query name, and returns the
// sorted list. Two not-exploitable states are recognized: NOT_EXPLOITABLE
// (the canonical state) and PROPOSED_NOT_EXPLOITABLE (which the legacy CxSAST
// tool also counted as suppressed for display purposes).
func groupNotExploitable(newRes []*models.ScanResult, cfg *compareOptions) []models.NotExploitableGroup {
	counts := map[string]*models.NotExploitableGroup{}
	for _, r := range newRes {
		if r.State != "NOT_EXPLOITABLE" && r.State != "PROPOSED_NOT_EXPLOITABLE" {
			continue
		}
		if !cfg.wantSeverity(r.Severity) || !cfg.wantEngine(r.Type) {
			continue
		}
		name := r.ScanResultData.QueryName
		if name == "" {
			name = "(unnamed)"
		}
		g, ok := counts[name]
		if !ok {
			g = &models.NotExploitableGroup{QueryName: name, Severity: r.Severity}
			counts[name] = g
		}
		g.Count++
	}
	out := make([]models.NotExploitableGroup, 0, len(counts))
	for _, g := range counts {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].QueryName < out[j].QueryName
	})
	return out
}

func identityFn(k models.IdentityKey) func(*models.ScanResult) string {
	switch k {
	case models.IdentityKeyResultHash:
		return func(r *models.ScanResult) string { return r.ScanResultData.ResultHash }
	default:
		return func(r *models.ScanResult) string { return r.SimilarityID }
	}
}
