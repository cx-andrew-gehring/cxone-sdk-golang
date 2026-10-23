// Package scans implements the high-level scan workflow front door.
//
// The package exposes three main types:
//
//   - [ScanInspector] — a typed view over a [models.ScanResponseModel] that
//     classifies the scan into Executing/Successful/Failed buckets, with
//     special handling for the "Partial" state per CLAUDE.md §9.4.
//
//   - [Waiter] — a polling helper that calls Refresh on an inspector at
//     a configurable interval until the scan reaches a terminal state.
//
//   - [Invoker] — a fluent builder for starting a new scan against a
//     [github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects.ProjectRepoConfig].
package scans

import (
	"context"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scans"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Backend bundles the dependencies a scan workflow handle needs.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}

// ScanInspector is a typed view over one Checkmarx One scan.
//
// It exposes the raw [models.ScanResponseModel] (via [Raw]) plus typed
// classification methods that bucket the scan into one of three states:
// Executing, Successful, or Failed. The "Partial" wire status is the
// interesting case — it means the scan completed but at least one engine
// returned an unusual result. The inspector handles this by walking the
// per-engine [models.StatusInfo] entries in StatusDetails and intersecting
// them against the engines that were originally requested when the scan
// was created. CLAUDE.md §9.4.
//
// Construct via [FromScanID] (which fetches the scan) or [FromScanModel]
// (which wraps an already-fetched scan response). The inspector is safe
// for concurrent reads but the [Refresh] method mutates the cached scan
// in place — serialize Refresh calls if you share the inspector across
// goroutines.
type ScanInspector struct {
	backend *Backend
	scan    *models.ScanResponseModel
}

// FromScanID is the async factory for [ScanInspector]. It issues
// GET /api/scans/{id} and returns the populated handle.
//
// CLAUDE.md §9.1 — domain objects whose construction needs a server fetch
// must use an async factory; this is that pattern.
func FromScanID(ctx context.Context, backend *Backend, scanID string) (*ScanInspector, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if scanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "scanID", Reason: "is required"}
	}
	scan, err := endpoints.Get(ctx, backend.Executor, backend.BaseURL, scanID)
	if err != nil {
		return nil, err
	}
	return &ScanInspector{backend: backend, scan: scan}, nil
}

// FromScanModel wraps an already-fetched scan response. No HTTP call is
// issued. Use this when you already have the [models.ScanResponseModel] in
// hand (e.g. from [github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scans.Iter]).
func FromScanModel(backend *Backend, scan *models.ScanResponseModel) (*ScanInspector, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if scan == nil {
		return nil, &cxerrors.ConfigurationError{Field: "scan", Reason: "is required"}
	}
	return &ScanInspector{backend: backend, scan: scan}, nil
}

// Raw returns the cached [models.ScanResponseModel] pointer. The caller
// must NOT mutate it; treat it as a read-only snapshot.
func (i *ScanInspector) Raw() *models.ScanResponseModel { return i.scan }

// ID returns the scan id.
func (i *ScanInspector) ID() string { return i.scan.ID }

// Status returns the verbatim wire-side status string.
func (i *ScanInspector) Status() models.ScanStatus { return i.scan.Status }

// Refresh re-fetches the scan from the platform and replaces the cached
// snapshot. Returns the inspector itself for chaining.
//
// Refresh is the only mutator on [ScanInspector]; serialize concurrent
// calls if you share the handle across goroutines.
func (i *ScanInspector) Refresh(ctx context.Context) (*ScanInspector, error) {
	scan, err := endpoints.Get(ctx, i.backend.Executor, i.backend.BaseURL, i.scan.ID)
	if err != nil {
		return i, err
	}
	i.scan = scan
	return i, nil
}

// Executing reports whether the scan is still in progress (Queued or Running),
// or — for the Partial state — whether at least one of the originally
// requested engines is still running.
func (i *ScanInspector) Executing() bool {
	switch string(i.scan.Status) {
	case models.ScanQueued, models.ScanRunning:
		return true
	case models.ScanPartial:
		return i.partialBucket() == bucketExecuting
	}
	return false
}

// Successful reports whether the scan finished successfully. For the
// Partial state, Successful is true only when EVERY originally requested
// engine reported Completed in StatusDetails.
func (i *ScanInspector) Successful() bool {
	switch string(i.scan.Status) {
	case models.ScanCompleted:
		return true
	case models.ScanPartial:
		return i.partialBucket() == bucketSuccessful
	}
	return false
}

// Failed reports whether the scan finished in a terminal failure state
// (Failed, Canceled). For the Partial state, Failed is true if any
// originally requested engine reported Failed (and no engines are still
// running).
func (i *ScanInspector) Failed() bool {
	switch string(i.scan.Status) {
	case models.ScanFailed, models.ScanCanceled:
		return true
	case models.ScanPartial:
		return i.partialBucket() == bucketFailed
	}
	return false
}

// StateMessage returns a human-readable summary of the scan's current
// state, suitable for inclusion in error messages and progress logs.
//
// For Partial scans the summary lists the per-engine statuses so users
// can see exactly which engine misbehaved.
func (i *ScanInspector) StateMessage() string {
	if string(i.scan.Status) != models.ScanPartial {
		return string(i.scan.Status)
	}
	parts := make([]string, 0, len(i.scan.StatusDetails))
	for _, sd := range i.scan.StatusDetails {
		entry := sd.Name + "=" + sd.Status
		if sd.Details != "" {
			entry += " (" + sd.Details + ")"
		}
		parts = append(parts, entry)
	}
	return "Partial: " + strings.Join(parts, ", ")
}

// partialBucket returns the derived bucket for a Partial scan by walking
// per-engine StatusDetails and intersecting against the engines that were
// originally requested when the scan was created (CLAUDE.md §9.4).
//
// Rules:
//   - If any originally-requested engine is still in an executing state
//     (Queued / Running), the scan is Executing.
//   - Else if EVERY originally-requested engine is in Completed, the scan
//     is Successful.
//   - Else (at least one originally-requested engine is in Failed or
//     Canceled, with the rest in mixed terminal states), the scan is Failed.
//
// Engines that were NOT in the original request are ignored — the platform
// sometimes returns extra entries (e.g. "auto-discovery") that should not
// poison the bucket calculation.
type partialBucketKind int

const (
	bucketExecuting partialBucketKind = iota
	bucketSuccessful
	bucketFailed
)

func (i *ScanInspector) partialBucket() partialBucketKind {
	requested := make(map[string]bool, len(i.scan.Engines))
	for _, e := range i.scan.Engines {
		requested[strings.ToLower(e)] = true
	}
	// If the scan response carries no engines list (defensive — shouldn't
	// happen for a real scan), treat all StatusDetails entries as relevant.
	considerAll := len(requested) == 0

	var anyExecuting, anyFailed, allCompleted bool
	allCompleted = true
	matched := 0

	for _, sd := range i.scan.StatusDetails {
		if !considerAll && !requested[strings.ToLower(sd.Name)] {
			continue
		}
		matched++
		switch sd.Status {
		case models.ScanQueued, models.ScanRunning:
			anyExecuting = true
			allCompleted = false
		case models.ScanFailed, models.ScanCanceled:
			anyFailed = true
			allCompleted = false
		case models.ScanCompleted:
			// Successful for this engine; allCompleted stays true unless
			// another engine flips it.
		default:
			// Unknown status — treat as failure to be conservative.
			anyFailed = true
			allCompleted = false
		}
	}

	if matched == 0 {
		// We couldn't reconcile any of the requested engines against the
		// status details. Conservatively bucket as Failed; the
		// StateMessage will surface the raw details for debugging.
		return bucketFailed
	}
	if anyExecuting {
		return bucketExecuting
	}
	if allCompleted {
		return bucketSuccessful
	}
	_ = anyFailed
	return bucketFailed
}
