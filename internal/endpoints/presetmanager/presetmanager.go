// Package presetmanager implements the Checkmarx One preset-manager API for
// SAST and IAC preset CRUD and query-family browsing.
//
// The preset-manager endpoints are engine-scoped: every path is prefixed with
// /api/preset-manager/{engine}/ where engine is "sast" or "iac". The low-level
// functions in this package take the engine as a parameter so callers can use
// a single code path for both.
package presetmanager

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func basePath(engine string) string { return "api/preset-manager/" + engine }

// ListPresets returns presets for the given engine.
// GET /api/preset-manager/{engine}/presets?limit=N&include_details=true → 200.
func ListPresets(ctx context.Context, e *transport.Executor, baseURL, engine string, limit int) (*models.PresetListResponse, error) {
	if engine == "" {
		return nil, &cxerrors.ConfigurationError{Field: "engine", Reason: "is required"}
	}
	q := url.Values{"include_details": {"true"}}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out models.PresetListResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, basePath(engine)+"/presets"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPreset returns a single preset by id.
// GET /api/preset-manager/{engine}/presets/{id} → 200.
func GetPreset(ctx context.Context, e *transport.Executor, baseURL, engine, presetID string) (*models.PresetManagerEntry, error) {
	if engine == "" || presetID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "engine/presetID", Reason: "both are required"}
	}
	var out models.PresetManagerEntry
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, basePath(engine)+"/presets/"+presetID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchPreset searches presets by name.
// GET /api/preset-manager/{engine}/presets?search-term=X&exact-match=true → 200.
func SearchPreset(ctx context.Context, e *transport.Executor, baseURL, engine, name string, exactMatch bool) (*models.PresetListResponse, error) {
	if engine == "" || name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "engine/name", Reason: "both are required"}
	}
	q := url.Values{
		"search-term":    {name},
		"exact-match":    {strconv.FormatBool(exactMatch)},
		"include_details": {"true"},
	}
	var out models.PresetListResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, basePath(engine)+"/presets"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePreset creates a new preset.
// POST /api/preset-manager/{engine}/presets → 201.
func CreatePreset(ctx context.Context, e *transport.Executor, baseURL, engine string, in *models.PresetCreateRequest) (*models.PresetManagerEntry, error) {
	if engine == "" || in == nil || in.Name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "engine/preset.Name", Reason: "is required"}
	}
	var out models.PresetManagerEntry
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, basePath(engine)+"/presets"),
		nil, in, []int{http.StatusCreated, http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePreset updates an existing preset.
// PUT /api/preset-manager/{engine}/presets/{id} → 204.
func UpdatePreset(ctx context.Context, e *transport.Executor, baseURL, engine, presetID string, in *models.PresetCreateRequest) error {
	if engine == "" || presetID == "" {
		return &cxerrors.ConfigurationError{Field: "engine/presetID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPut,
		transport.JoinURL(baseURL, basePath(engine)+"/presets/"+presetID),
		nil, in, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// DeletePreset removes a preset.
// DELETE /api/preset-manager/{engine}/presets/{id} → 204.
func DeletePreset(ctx context.Context, e *transport.Executor, baseURL, engine, presetID string) error {
	if engine == "" || presetID == "" {
		return &cxerrors.ConfigurationError{Field: "engine/presetID", Reason: "both are required"}
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, basePath(engine)+"/presets/"+presetID),
		nil, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// ListQueryFamilies returns the query-family names for an engine.
// GET /api/preset-manager/{engine}/query-families → 200.
func ListQueryFamilies(ctx context.Context, e *transport.Executor, baseURL, engine string) ([]string, error) {
	if engine == "" {
		return nil, &cxerrors.ConfigurationError{Field: "engine", Reason: "is required"}
	}
	var out []string
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, basePath(engine)+"/query-families"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSASTQueryFamilyContents returns the queries in a SAST query family.
// GET /api/preset-manager/sast/query-families/{family}/queries → 200.
func GetSASTQueryFamilyContents(ctx context.Context, e *transport.Executor, baseURL, family string) (*models.SASTQueryCollection, error) {
	if family == "" {
		return nil, &cxerrors.ConfigurationError{Field: "family", Reason: "is required"}
	}
	var out models.SASTQueryCollection
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, basePath("sast")+"/query-families/"+url.PathEscape(family)+"/queries"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetIACQueryFamilyContents returns the queries in an IAC query family.
// GET /api/preset-manager/iac/query-families/{family}/queries → 200.
func GetIACQueryFamilyContents(ctx context.Context, e *transport.Executor, baseURL, family string) (*models.IACQueryCollection, error) {
	if family == "" {
		return nil, &cxerrors.ConfigurationError{Field: "family", Reason: "is required"}
	}
	var out models.IACQueryCollection
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, basePath("iac")+"/query-families/"+url.PathEscape(family)+"/queries"),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
