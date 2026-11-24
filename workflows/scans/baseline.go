package scans

import (
	"context"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scans"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// baselinePageSize bounds how many newest-first scans LatestCompleted
// inspects per request. The server is asked for Completed scans only; the
// client-side status check is defense in depth.
const baselinePageSize = 20

// LatestCompleted returns the newest scan of projectID on branch whose status
// is Completed, or (nil, nil) when there is none. Running, Queued, Failed,
// Canceled and Partial scans are never returned, so the result is safe to use
// as a diff baseline.
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
	q.Set("sort", "-created_at")
	q.Set("limit", strconv.Itoa(baselinePageSize))
	q.Set("offset", "0")

	page, err := endpoints.List(ctx, b.Executor, b.BaseURL, q)
	if err != nil {
		return nil, err
	}
	for i := range page.Scans {
		s := page.Scans[i]
		if s.Status == models.ScanCompleted {
			return &s, nil
		}
	}
	return nil, nil
}

// NewFindings returns the findings present in newScanID but not in oldScanID,
// reusing [Backend.Compare] for fetching and identity matching (default
// identity: similarityId; options are accepted as for Compare). At most
// perEngineCap findings are returned per engine ([models.ScanResult.Type]);
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
	perEngine := map[string]int{}
	emitted := map[string]struct{}{}
	for _, r := range newRes {
		if !cfg.wantSeverity(r.Severity) || !cfg.wantEngine(r.Type) {
			continue
		}
		id := idOf(r)
		if _, ok := seen[id]; ok {
			continue
		}
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
