package scans

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scans"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// baselinePageSize is the page size used to walk the filtered scan list.
const baselinePageSize = 100

// LatestCompleted returns the newest scan of projectID on branch whose status
// is Completed, or (nil, nil) when there is none. Running, Queued, Failed,
// Canceled and Partial scans are never returned, so the result is safe to use
// as a diff baseline.
//
// The result does not depend on server-side ordering: every page of the
// filtered list is walked and the Completed scan with the greatest CreatedAt
// wins. A scan with a missing (zero) CreatedAt only wins when no scan has a
// timestamp; ties keep the first one seen.
func LatestCompleted(ctx context.Context, b *Backend, projectID, branch string) (*models.ScanResponseModel, error) {
	if b == nil || b.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if projectID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	q := url.Values{}
	q.Set("project-id", projectID)
	if branch != "" {
		q.Set("branch", branch)
	}
	q.Set("statuses", models.ScanCompleted)

	it := endpoints.Iter(b.Executor, b.BaseURL, q, pagination.Config{PageSize: baselinePageSize})
	all, err := pagination.Collect(ctx, it)
	if err != nil {
		return nil, err
	}
	var best *models.ScanResponseModel
	for i := range all {
		s := &all[i]
		if s.Status != models.ScanCompleted {
			continue
		}
		if best == nil || s.CreatedAt.After(best.CreatedAt) {
			best = s
		}
	}
	if best == nil {
		return nil, nil
	}
	out := *best
	return &out, nil
}

// NewFindings returns the findings present in newScanID but not in oldScanID,
// reusing [Backend.Compare] for fetching and identity matching (default
// identity: similarityId; options are accepted as for Compare). At most
// perEngineCap findings are returned per engine ([models.ScanResult.Type]),
// chosen deterministically (severity descending Critical>High>Medium>Low>Info,
// then identity, then result ID ascending, independent of fetch order);
// when any engine exceeds the cap the extra findings are dropped and
// [models.ScanComparison.Truncated] is set. Counts in Totals and BySeverity
// remain uncapped. A perEngineCap <= 0 means no cap.
func NewFindings(ctx context.Context, b *Backend, oldScanID, newScanID string, perEngineCap int, opts ...CompareOption) (*models.ScanComparison, error) {
	cmp, oldRes, newRes, cfg, err := b.compare(ctx, oldScanID, newScanID, opts...)
	if err != nil {
		return nil, err
	}
	idOf := identityFn(cfg.identityKey)
	seen := make(map[string]struct{}, len(oldRes))
	for _, r := range oldRes {
		seen[idOf(r)] = struct{}{}
	}
	var cands []*models.ScanResult
	for _, r := range newRes {
		if !cfg.wantSeverity(r.Severity) || !cfg.wantEngine(r.Type) {
			continue
		}
		if _, ok := seen[idOf(r)]; ok {
			continue
		}
		cands = append(cands, r)
	}
	// Deterministic order so the same findings survive the cap on every call:
	// severity descending, then identity, then result ID ascending.
	sort.SliceStable(cands, func(i, j int) bool {
		ri, rj := severityRank(cands[i].Severity), severityRank(cands[j].Severity)
		if ri != rj {
			return ri > rj
		}
		ii, ij := idOf(cands[i]), idOf(cands[j])
		if ii != ij {
			return ii < ij
		}
		return cands[i].ID < cands[j].ID
	})
	perEngine := map[string]int{}
	emitted := map[string]struct{}{}
	for _, r := range cands {
		id := idOf(r)
		if _, dup := emitted[id]; dup {
			continue
		}
		emitted[id] = struct{}{}
		if perEngineCap > 0 && perEngine[r.Type] >= perEngineCap {
			cmp.Truncated = true
			continue
		}
		perEngine[r.Type]++
		cmp.NewFindings = append(cmp.NewFindings, r)
	}
	return cmp, nil
}

func severityRank(s string) int {
	switch strings.ToUpper(s) {
	case "CRITICAL":
		return 5
	case "HIGH":
		return 4
	case "MEDIUM":
		return 3
	case "LOW":
		return 2
	case "INFO":
		return 1
	}
	return 0
}
