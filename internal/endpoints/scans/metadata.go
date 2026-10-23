package scans

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// GetConfiguration returns the scan-level configuration for a project+scan.
// GET /api/configuration/scan?project-id=X&scan-id=Y → 200.
func GetConfiguration(ctx context.Context, e *transport.Executor, baseURL, projectID, scanID string) ([]models.ProjectConfiguration, error) {
	q := url.Values{}
	if projectID != "" {
		q.Set("project-id", projectID)
	}
	if scanID != "" {
		q.Set("scan-id", scanID)
	}
	var out []models.ProjectConfiguration
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, "api/configuration/scan"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSummary returns the high-level scan status summary.
// GET /api/scans/summary → 200.
func GetSummary(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) ([]models.ScanSummary, error) {
	var out []models.ScanSummary
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/summary"),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSASTAggregate returns the SAST aggregate summary for a scan.
// GET /api/sast-scan-summary/aggregate?scan-id=X&... → 200.
func GetSASTAggregate(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) ([]models.SASTAggregateSummary, error) {
	var out []models.SASTAggregateSummary
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, "api/sast-scan-summary/aggregate"),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
