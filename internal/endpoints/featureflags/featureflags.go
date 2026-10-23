// Package featureflags implements the Checkmarx One feature-flags lookup
// endpoint.
//
// Feature flags gate platform behavior at the tenant level. Most callers
// should not need this — the high-level workflow layer queries the flags it
// cares about itself. The low-level endpoint is exposed here for power users
// and for the workflow layer to use.
package featureflags

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the feature-flags endpoint.
const Path = "api/flags"

// List returns the full set of feature flags visible to the caller.
// GET /api/flags → 200.
//
// tenantID, if non-empty, is appended as a "filter" query parameter so the
// platform can scope the response — match the value the platform expects
// (typically the tenant UUID extracted from the access token claims).
func List(ctx context.Context, e *transport.Executor, baseURL, tenantID string) ([]models.FeatureFlag, error) {
	q := url.Values{}
	if tenantID != "" {
		q.Set("filter", tenantID)
	}
	var out []models.FeatureFlag
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches a single feature flag by name. GET /api/flags/{name} → 200.
func Get(ctx context.Context, e *transport.Executor, baseURL, tenantID, name string) (*models.FeatureFlag, error) {
	q := url.Values{}
	if tenantID != "" {
		q.Set("filter", tenantID)
	}
	var out models.FeatureFlag
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+name),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
