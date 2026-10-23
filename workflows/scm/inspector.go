package scm

import (
	"context"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scm"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// statusClass is the terminal-state classification of a raw migrationStatus
// string.
type statusClass int

const (
	// classExecuting is also the fallback for statuses we do not recognize.
	// That direction is deliberate: treating an unknown status as "still
	// running" makes a waiter stall until its deadline, whereas treating it
	// as terminal would report a conversion as finished when it may not be.
	// Stalling is recoverable; a false success is not.
	classExecuting statusClass = iota
	classCompleted
	classFailed
)

// The conversion API documents only "OK". The remaining strings are the
// plausible neighbours drawn from the other Checkmarx One async APIs; each is
// matched case-insensitively, and anything unmatched falls back to
// classExecuting.
var (
	completedStatuses = map[string]struct{}{
		"ok": {}, "completed": {}, "complete": {}, "success": {},
		"succeeded": {}, "done": {}, "finished": {},
	}
	failedStatuses = map[string]struct{}{
		"failed": {}, "failure": {}, "error": {}, "canceled": {},
		"cancelled": {}, "aborted": {},
	}
)

// ConversionInspector wraps a [models.SCMConversionStatus] with typed
// terminal-state predicates (CLAUDE.md §9.4).
//
// Construct it with [FromProcessID], then either poll manually via
// [ConversionInspector.Refresh] or hand it to [WaitUntilComplete].
type ConversionInspector struct {
	backend   *Backend
	processID string
	data      *models.SCMConversionStatus
}

// FromProcessID is the async factory for [ConversionInspector]. It issues one
// status request and returns a populated inspector.
//
// Conversion process data is retained for 7 days; older process ids fail here
// rather than reporting a terminal state.
func FromProcessID(ctx context.Context, backend *Backend, processID string) (*ConversionInspector, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if processID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "processID", Reason: "is required"}
	}
	data, err := endpoints.GetConversionStatus(ctx, backend.Executor, backend.BaseURL, processID)
	if err != nil {
		return nil, err
	}
	return &ConversionInspector{backend: backend, processID: processID, data: data}, nil
}

// Data returns the underlying status snapshot. May be nil.
func (i *ConversionInspector) Data() *models.SCMConversionStatus { return i.data }

// ID returns the conversion process id.
func (i *ConversionInspector) ID() string { return i.processID }

// Status returns the raw migrationStatus string.
func (i *ConversionInspector) Status() string {
	if i.data == nil {
		return ""
	}
	return i.data.Status
}

// FailedProjects returns the repos that failed to convert.
func (i *ConversionInspector) FailedProjects() []string {
	if i.data == nil {
		return nil
	}
	return i.data.FailedProjectList
}

// classify maps the raw status onto a statusClass.
func (i *ConversionInspector) classify() statusClass {
	s := strings.ToLower(strings.TrimSpace(i.Status()))
	if _, ok := completedStatuses[s]; ok {
		return classCompleted
	}
	if _, ok := failedStatuses[s]; ok {
		return classFailed
	}
	return classExecuting
}

// Completed reports whether the process finished with every project
// converted. A process that reports success but lists failed projects is not
// Completed — see [ConversionInspector.Failed].
func (i *ConversionInspector) Completed() bool {
	return i.classify() == classCompleted && len(i.FailedProjects()) == 0
}

// Failed reports whether the process ended in a failure state, or finished
// with at least one project in failedProjectList.
//
// The second condition matters because the API reports a partially-successful
// batch with an overall status of "OK": for a caller converting a single
// project, that project appearing in failedProjectList is an outright failure
// regardless of the process-level status.
func (i *ConversionInspector) Failed() bool {
	switch i.classify() {
	case classFailed:
		return true
	case classCompleted:
		return len(i.FailedProjects()) > 0
	default:
		return false
	}
}

// Executing reports whether the process is still running. An unrecognized
// status counts as executing.
func (i *ConversionInspector) Executing() bool { return !i.Completed() && !i.Failed() }

// Terminal reports whether the process has reached any final state.
func (i *ConversionInspector) Terminal() bool { return i.Completed() || i.Failed() }

// Refresh re-fetches the process state and updates the internal snapshot.
// The receiver is returned for convenience, including on error.
func (i *ConversionInspector) Refresh(ctx context.Context) (*ConversionInspector, error) {
	data, err := endpoints.GetConversionStatus(ctx, i.backend.Executor, i.backend.BaseURL, i.processID)
	if err != nil {
		return i, err
	}
	i.data = data
	return i, nil
}
