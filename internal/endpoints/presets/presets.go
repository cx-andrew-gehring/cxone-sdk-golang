// Package presets implements the Checkmarx One presets + queries endpoints
// (low-level layer).
//
// ast-cli does not wrap these endpoints — the CLI uses preset NAMES as
// opaque strings rather than fetching the catalog. The path constants below
// follow common Checkmarx One REST conventions but should be VERIFIED
// against the live swagger at https://ast.checkmarx.net/spec/v1 before
// relying on them in production.
//
// The high-level [PresetReader] in workflows/presets uses these primitives
// to build the multi-level cache mandated by CLAUDE.md §9.5.
//
// TODO(spec): cross-check every Path const below against the live swagger.
package presets

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path constants. TODO(spec): verify.
const (
	PresetsPath = "api/presets"
	QueriesPath = "api/queries"
)

// ListPresets returns one page of preset descriptors.
// GET /api/presets → 200.
func ListPresets(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (*models.PresetCollection, error) {
	var out models.PresetCollection
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, PresetsPath),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPreset fetches one preset detail by id.
// GET /api/presets/{id} → 200.
func GetPreset(ctx context.Context, e *transport.Executor, baseURL, presetID string) (*models.PresetDetail, error) {
	var out models.PresetDetail
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, PresetsPath+"/"+presetID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListQueries returns one page of SAST query descriptors.
// GET /api/queries → 200.
func ListQueries(ctx context.Context, e *transport.Executor, baseURL string, query url.Values) (*models.QueryCollection, error) {
	var out models.QueryCollection
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, QueriesPath),
		query, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
