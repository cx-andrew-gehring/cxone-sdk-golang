package migration

import (
	"context"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/migration"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// ImportInspector wraps a [models.DataImport] with typed terminal-state
// properties, modeled after ScanInspector (CLAUDE.md §9.4).
type ImportInspector struct {
	backend  *Backend
	importID string
	data     *models.DataImport
}

// FromImportID constructs an inspector by fetching the current import state.
func FromImportID(ctx context.Context, backend *Backend, importID string) (*ImportInspector, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if importID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "importID", Reason: "is required"}
	}
	data, err := endpoints.Get(ctx, backend.Executor, backend.BaseURL, importID)
	if err != nil {
		return nil, err
	}
	return &ImportInspector{backend: backend, importID: importID, data: data}, nil
}

// Data returns the underlying import model snapshot.
func (i *ImportInspector) Data() *models.DataImport { return i.data }

// ID returns the migration id.
func (i *ImportInspector) ID() string { return i.importID }

// Status returns the raw status string.
func (i *ImportInspector) Status() string {
	if i.data == nil {
		return ""
	}
	return i.data.Status
}

// Completed returns true when the import finished successfully (or with
// partial results).
func (i *ImportInspector) Completed() bool {
	s := i.Status()
	return s == models.ImportStatusCompleted || s == models.ImportStatusPartial
}

// Failed returns true when the import ended in a failure state.
func (i *ImportInspector) Failed() bool {
	s := i.Status()
	return s == models.ImportStatusFailed || s == models.ImportStatusBlank
}

// Terminal returns true when the import has reached any final state.
func (i *ImportInspector) Terminal() bool { return i.Completed() || i.Failed() }

// Refresh re-fetches the import state from the server and updates the
// internal snapshot.
func (i *ImportInspector) Refresh(ctx context.Context) (*ImportInspector, error) {
	data, err := endpoints.Get(ctx, i.backend.Executor, i.backend.BaseURL, i.importID)
	if err != nil {
		return i, err
	}
	i.data = data
	return i, nil
}
