// Package analytics implements the Checkmarx One analytics KPI API.
//
// All analytics queries go through a single POST endpoint that accepts a
// "kpi" field discriminating the response shape. This package exposes one
// typed function per KPI instead of surfacing the raw discriminator; the
// workflow layer (workflows/analytics, not yet implemented) layers
// dashboards and charting helpers on top.
//
// Part of the SDK's low-level endpoint layer (CLAUDE.md §8).
package analytics

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the analytics POST endpoint, relative to the API host root.
const Path = "api/data_analytics/analyticsAPI/v1"

// Fetch is the generic primitive every typed helper in this package calls.
// Most users should prefer the typed helpers; Fetch is exposed for callers
// that need to issue a KPI Checkmarx One has added that the SDK does not
// yet cover.
//
// limit and offset are honored only by the KPIs that support paging
// (allVulnerabilities, mostCommonVulnerabilities, mostAgingVulnerabilities).
func Fetch(ctx context.Context, e *transport.Executor, baseURL, kpi string, limit uint64, offset *uint64, filter models.AnalyticsFilter, out any) error {
	if kpi == "" {
		return &cxerrors.ConfigurationError{Field: "kpi", Reason: "is required"}
	}
	body := models.AnalyticsRequest{
		AnalyticsFilter: filter,
		KPI:             kpi,
		Limit:           limit,
		Offset:          offset,
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, body, []int{http.StatusOK}, out)
}

// fetchRaw is the internal variant that returns the raw response bytes so
// typed helpers can unmarshal into KPI-specific envelopes without having the
// generic Fetch double-unmarshal.
func fetchRaw(ctx context.Context, e *transport.Executor, baseURL, kpi string, limit uint64, offset *uint64, filter models.AnalyticsFilter) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := Fetch(ctx, e, baseURL, kpi, limit, offset, filter, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// GetVulnerabilitiesBySeverityTotal returns the vulnerabilitiesBySeverityTotal KPI.
func GetVulnerabilitiesBySeverityTotal(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) (*models.AnalyticsDistributionStats, error) {
	return getDistribution(ctx, e, baseURL, "vulnerabilitiesBySeverityTotal", filter)
}

// GetVulnerabilitiesByStateTotal returns the vulnerabilitiesByStateTotal KPI.
func GetVulnerabilitiesByStateTotal(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) (*models.AnalyticsDistributionStats, error) {
	return getDistribution(ctx, e, baseURL, "vulnerabilitiesByStateTotal", filter)
}

// GetVulnerabilitiesByStatusTotal returns the vulnerabilitiesByStatusTotal KPI.
func GetVulnerabilitiesByStatusTotal(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) (*models.AnalyticsDistributionStats, error) {
	return getDistribution(ctx, e, baseURL, "vulnerabilitiesByStatusTotal", filter)
}

func getDistribution(ctx context.Context, e *transport.Executor, baseURL, kpi string, filter models.AnalyticsFilter) (*models.AnalyticsDistributionStats, error) {
	raw, err := fetchRaw(ctx, e, baseURL, kpi, 0, nil, filter)
	if err != nil {
		return nil, err
	}
	var out models.AnalyticsDistributionStats
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode " + kpi + ": " + err.Error(), Cause: err,
		}
	}
	return &out, nil
}

// GetVulnerabilitiesBySeverityAndStateTotal returns the
// vulnerabilitiesBySeverityAndStateTotal KPI as a flat list.
func GetVulnerabilitiesBySeverityAndStateTotal(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) ([]models.AnalyticsSeverityAndStateEntry, error) {
	raw, err := fetchRaw(ctx, e, baseURL, "vulnerabilitiesBySeverityAndStateTotal", 0, nil, filter)
	if err != nil {
		return nil, err
	}
	var out []models.AnalyticsSeverityAndStateEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode severity-and-state: " + err.Error(), Cause: err,
		}
	}
	return out, nil
}

// GetVulnerabilitiesByAgingTotal returns the agingTotal KPI.
func GetVulnerabilitiesByAgingTotal(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) ([]models.AnalyticsAgingEntry, error) {
	raw, err := fetchRaw(ctx, e, baseURL, "agingTotal", 0, nil, filter)
	if err != nil {
		return nil, err
	}
	var env models.AnalyticsAgingResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode aging: " + err.Error(), Cause: err,
		}
	}
	return env.AgingAndSeverities, nil
}

// GetVulnerabilitiesBySeverityOvertime returns the
// vulnerabilitiesBySeverityOvertime KPI.
func GetVulnerabilitiesBySeverityOvertime(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) ([]models.AnalyticsOverTimeStats, error) {
	return getOverTime(ctx, e, baseURL, "vulnerabilitiesBySeverityOvertime", filter)
}

// GetFixedVulnerabilitiesBySeverityOvertime returns the
// fixedVulnerabilitiesBySeverityOvertime KPI.
func GetFixedVulnerabilitiesBySeverityOvertime(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) ([]models.AnalyticsOverTimeStats, error) {
	return getOverTime(ctx, e, baseURL, "fixedVulnerabilitiesBySeverityOvertime", filter)
}

func getOverTime(ctx context.Context, e *transport.Executor, baseURL, kpi string, filter models.AnalyticsFilter) ([]models.AnalyticsOverTimeStats, error) {
	raw, err := fetchRaw(ctx, e, baseURL, kpi, 0, nil, filter)
	if err != nil {
		return nil, err
	}
	var env models.AnalyticsOverTimeResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode " + kpi + ": " + err.Error(), Cause: err,
		}
	}
	return env.Distribution, nil
}

// GetMeanTimeToResolution returns the meanTimeToResolution KPI.
func GetMeanTimeToResolution(ctx context.Context, e *transport.Executor, baseURL string, filter models.AnalyticsFilter) (*models.AnalyticsMeanTimeStats, error) {
	raw, err := fetchRaw(ctx, e, baseURL, "meanTimeToResolution", 0, nil, filter)
	if err != nil {
		return nil, err
	}
	var out models.AnalyticsMeanTimeStats
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode mttr: " + err.Error(), Cause: err,
		}
	}
	return &out, nil
}

// GetMostCommonVulnerabilities returns the top-N most common vulnerabilities.
func GetMostCommonVulnerabilities(ctx context.Context, e *transport.Executor, baseURL string, limit uint64, filter models.AnalyticsFilter) ([]models.AnalyticsVulnerabilityStats, error) {
	return getVulnStats(ctx, e, baseURL, "mostCommonVulnerabilities", limit, filter)
}

// GetMostAgingVulnerabilities returns the top-N aging vulnerabilities.
func GetMostAgingVulnerabilities(ctx context.Context, e *transport.Executor, baseURL string, limit uint64, filter models.AnalyticsFilter) ([]models.AnalyticsVulnerabilityStats, error) {
	return getVulnStats(ctx, e, baseURL, "mostAgingVulnerabilities", limit, filter)
}

func getVulnStats(ctx context.Context, e *transport.Executor, baseURL, kpi string, limit uint64, filter models.AnalyticsFilter) ([]models.AnalyticsVulnerabilityStats, error) {
	raw, err := fetchRaw(ctx, e, baseURL, kpi, limit, nil, filter)
	if err != nil {
		return nil, err
	}
	var out []models.AnalyticsVulnerabilityStats
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode " + kpi + ": " + err.Error(), Cause: err,
		}
	}
	return out, nil
}

// GetAllVulnerabilities returns a paged list of every vulnerability matching
// the filter. The allVulnerabilities KPI requires both limit and offset.
func GetAllVulnerabilities(ctx context.Context, e *transport.Executor, baseURL string, limit, offset uint64, filter models.AnalyticsFilter) ([]models.AnalyticsVulnerabilityStats, error) {
	raw, err := fetchRaw(ctx, e, baseURL, "allVulnerabilities", limit, &offset, filter)
	if err != nil {
		return nil, err
	}
	var env models.AnalyticsAllVulnerabilitiesResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode all-vulnerabilities: " + err.Error(), Cause: err,
		}
	}
	return env.AllVulnerabilities, nil
}

// GetIDETotal returns the ideTotal KPI. The endpoint does not accept a
// filter (per Checkmarx One documentation).
func GetIDETotal(ctx context.Context, e *transport.Executor, baseURL string) ([]models.AnalyticsIDEStatEntry, error) {
	raw, err := fetchRaw(ctx, e, baseURL, "ideTotal", 0, nil, models.AnalyticsFilter{})
	if err != nil {
		return nil, err
	}
	var env models.AnalyticsIDETotalResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode ide-total: " + err.Error(), Cause: err,
		}
	}
	return env.IDEData, nil
}

// GetIDEOverTime returns the ideOvertime KPI.
func GetIDEOverTime(ctx context.Context, e *transport.Executor, baseURL string) ([]models.AnalyticsIDEOverTimeDistribution, error) {
	raw, err := fetchRaw(ctx, e, baseURL, "ideOvertime", 0, nil, models.AnalyticsFilter{})
	if err != nil {
		return nil, err
	}
	var env models.AnalyticsIDEOverTimeResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodPost, URL: Path,
			Reason: "decode ide-overtime: " + err.Error(), Cause: err,
		}
	}
	return env.Distribution, nil
}
