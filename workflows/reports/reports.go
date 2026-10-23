// Package reports implements the high-level reports workflow.
//
// Generating a Checkmarx One report is a three-step dance — request, poll,
// download — that the low-level [internal/endpoints/reports] package
// exposes as discrete primitives. This package wraps that dance behind a
// fluent builder + waiter + download convenience so application code can
// say:
//
//	url, err := client.Reports().NewJSONReport().
//	    ForScan(scanID, projectID, "main").
//	    WithSections("scan-summary", "results").
//	    WithScanners("sast", "sca").
//	    Generate(ctx).
//	    WaitAndDownload(ctx, dest)
//
// CLAUDE.md §9.4 calls for fluent builders + waiter conveniences for
// long-running platform operations.
package reports

import (
	"context"
	"io"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/reports"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/observability"
)

// Backend bundles the dependencies the workflow needs.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}

// Format selects which report format the builder targets.
type Format int

const (
	// FormatJSON targets POST /api/reports/json.
	FormatJSON Format = iota
	// FormatPDF targets POST /api/reports/pdf.
	FormatPDF
)

// Builder is the fluent builder for one report request. Construct via
// [NewJSONReport] or [NewPDFReport]; chain configuration setters; call
// [Builder.Generate] to enqueue the report and obtain a [Handle] for
// polling and download.
type Builder struct {
	backend *Backend
	format  Format

	reportName string
	reportType string
	fileFormat string
	data       models.ReportData

	configErr error
}

// NewJSONReport returns a fluent builder for a JSON report.
//
// Reasonable defaults: ReportName="cxone-report", ReportType="ui",
// FileFormat="json". Override via [Builder.WithReportName] / [Builder.WithReportType] /
// [Builder.WithFileFormat].
func NewJSONReport(backend *Backend) *Builder {
	if backend == nil || backend.Executor == nil {
		return &Builder{configErr: &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}}
	}
	return &Builder{
		backend:    backend,
		format:     FormatJSON,
		reportName: "cxone-report",
		reportType: "ui",
		fileFormat: "json",
	}
}

// NewPDFReport returns a fluent builder for a PDF report.
func NewPDFReport(backend *Backend) *Builder {
	if backend == nil || backend.Executor == nil {
		return &Builder{configErr: &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}}
	}
	return &Builder{
		backend:    backend,
		format:     FormatPDF,
		reportName: "cxone-report",
		reportType: "ui",
		fileFormat: "pdf",
	}
}

// ForScan binds the report to a scan + project + branch. Required.
func (b *Builder) ForScan(scanID, projectID, branchName string) *Builder {
	b.data.ScanID = scanID
	b.data.ProjectID = projectID
	b.data.BranchName = branchName
	return b
}

// WithSections selects which report sections to include (e.g.
// "scan-summary", "results", "vulnerability-details"). Optional.
func (b *Builder) WithSections(sections ...string) *Builder {
	b.data.Sections = append([]string(nil), sections...)
	return b
}

// WithScanners restricts the report to specific engines (e.g. "sast", "sca").
// When empty, all engines from the scan are included.
func (b *Builder) WithScanners(scanners ...string) *Builder {
	b.data.Scanners = append([]string(nil), scanners...)
	return b
}

// WithEmail asks the platform to send the completed report by email to the
// supplied addresses. Optional.
func (b *Builder) WithEmail(addrs ...string) *Builder {
	b.data.Email = append([]string(nil), addrs...)
	return b
}

// WithHost overrides the host the platform should use when constructing
// links inside the report (e.g. result deep links). Defaults to the
// platform's own host. Optional.
func (b *Builder) WithHost(host string) *Builder {
	b.data.Host = host
	return b
}

// WithReportName overrides the default "cxone-report" name.
func (b *Builder) WithReportName(name string) *Builder {
	b.reportName = name
	return b
}

// WithReportType overrides the default "ui" report type.
func (b *Builder) WithReportType(rtype string) *Builder {
	b.reportType = rtype
	return b
}

// WithFileFormat overrides the file format. Set this only if the platform
// supports a non-default format for the report type/format combination
// you're requesting.
func (b *Builder) WithFileFormat(format string) *Builder {
	b.fileFormat = format
	return b
}

// Generate enqueues the report and returns a [Handle] for polling and
// download. Validation runs first; missing required fields surface as
// *cxerrors.ConfigurationError.
func (b *Builder) Generate(ctx context.Context) (*Handle, error) {
	if b.configErr != nil {
		return nil, b.configErr
	}
	if b.data.ScanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "ScanID", Reason: "is required"}
	}
	if b.data.ProjectID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "ProjectID", Reason: "is required"}
	}
	if b.data.BranchName == "" {
		return nil, &cxerrors.ConfigurationError{Field: "BranchName", Reason: "is required"}
	}
	switch b.format {
	case FormatJSON:
		req := &models.JSONReportRequest{
			ReportName: b.reportName,
			ReportType: b.reportType,
			FileFormat: b.fileFormat,
			Data:       b.data,
		}
		resp, err := endpoints.CreateJSON(ctx, b.backend.Executor, b.backend.BaseURL, req)
		if err != nil {
			return nil, err
		}
		return &Handle{backend: b.backend, format: FormatJSON, reportID: resp.ReportID}, nil
	case FormatPDF:
		req := &models.PDFReportRequest{
			ReportName: b.reportName,
			ReportType: b.reportType,
			FileFormat: b.fileFormat,
			Data:       b.data,
		}
		resp, err := endpoints.CreatePDF(ctx, b.backend.Executor, b.backend.BaseURL, req)
		if err != nil {
			return nil, err
		}
		return &Handle{backend: b.backend, format: FormatPDF, reportID: resp.ReportID}, nil
	}
	return nil, &cxerrors.ConfigurationError{Field: "format", Reason: "unknown"}
}

// Handle is the polling/download handle returned by [Builder.Generate].
type Handle struct {
	backend  *Backend
	format   Format
	reportID string

	// Last polled status — populated by Wait / Poll. Tracked so callers
	// can inspect intermediate states.
	lastStatus string
	lastURL    string
}

// ReportID returns the platform-assigned report id.
func (h *Handle) ReportID() string { return h.reportID }

// Status returns the last observed status string ("requested", "started",
// "completed", "failed"), or "" if Poll/Wait have not been called yet.
func (h *Handle) Status() string { return h.lastStatus }

// URL returns the download URL surfaced by the latest Poll, if the report
// has reached the completed state. Otherwise empty.
func (h *Handle) URL() string { return h.lastURL }

// Poll fetches the current status of the report from the platform.
func (h *Handle) Poll(ctx context.Context) error {
	switch h.format {
	case FormatJSON:
		resp, err := endpoints.PollJSON(ctx, h.backend.Executor, h.backend.BaseURL, h.reportID)
		if err != nil {
			return err
		}
		h.lastStatus = resp.Status
		h.lastURL = resp.URL
		return nil
	case FormatPDF:
		resp, err := endpoints.PollPDF(ctx, h.backend.Executor, h.backend.BaseURL, h.reportID)
		if err != nil {
			return err
		}
		h.lastStatus = resp.Status
		h.lastURL = resp.URL
		return nil
	}
	return &cxerrors.ConfigurationError{Field: "format", Reason: "unknown"}
}

// WaitOptions configures [Handle.Wait].
type WaitOptions struct {
	// PollInterval is the time to wait between successive Poll calls.
	// Default 5 seconds.
	PollInterval time.Duration
	// MaxDuration is an upper bound on total wait time. Zero means
	// "honor only ctx".
	MaxDuration time.Duration
	// Logger receives one cxone.report.poll event per Poll call.
	Logger observability.Logger
}

// Wait polls the report at PollInterval intervals until it reaches a
// terminal state (completed or failed) and returns. On terminal failure
// Wait returns a *cxerrors.ReportError; on completion it returns nil and
// the caller can call [Handle.Download] (or simply read [Handle.URL] for
// the platform-hosted download link).
func (h *Handle) Wait(ctx context.Context, opts WaitOptions) error {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = observability.NopLogger{}
	}

	deadline, hasDeadline := computeDeadline(ctx, opts.MaxDuration)

	for {
		if err := h.Poll(ctx); err != nil {
			return err
		}
		opts.Logger.Info(ctx, observability.EventReportPoll,
			observability.F("reportId", h.reportID),
			observability.F("status", h.lastStatus),
		)
		switch h.lastStatus {
		case models.ReportStatusCompleted:
			return nil
		case models.ReportStatusFailed:
			return &cxerrors.ReportError{
				ReportID: h.reportID,
				Reason:   "platform reported failed status",
			}
		}
		if err := sleepUntil(ctx, opts.PollInterval, deadline, hasDeadline); err != nil {
			return err
		}
	}
}

// Download streams the completed report body. Caller MUST close the
// returned ReadCloser.
//
// Should be called only after [Handle.Wait] has returned nil (i.e. the
// status is "completed"). Calling earlier may return a 404 from the
// platform.
func (h *Handle) Download(ctx context.Context) (io.ReadCloser, error) {
	switch h.format {
	case FormatJSON:
		return endpoints.DownloadJSON(ctx, h.backend.Executor, h.backend.BaseURL, h.reportID)
	case FormatPDF:
		return endpoints.DownloadPDF(ctx, h.backend.Executor, h.backend.BaseURL, h.reportID)
	}
	return nil, &cxerrors.ConfigurationError{Field: "format", Reason: "unknown"}
}

// WaitAndDownload is the convenience method that combines [Handle.Wait]
// and [Handle.Download] into a single call. The completed report bytes
// are written to dest and the number of bytes copied is returned.
func (h *Handle) WaitAndDownload(ctx context.Context, dest io.Writer, opts WaitOptions) (int64, error) {
	if err := h.Wait(ctx, opts); err != nil {
		return 0, err
	}
	rc, err := h.Download(ctx)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	return io.Copy(dest, rc)
}

// computeDeadline returns the absolute deadline at which Wait should stop.
func computeDeadline(ctx context.Context, maxDur time.Duration) (time.Time, bool) {
	if maxDur > 0 {
		return time.Now().Add(maxDur), true
	}
	if d, ok := ctx.Deadline(); ok {
		return d, true
	}
	return time.Time{}, false
}

func sleepUntil(ctx context.Context, interval time.Duration, deadline time.Time, hasDeadline bool) error {
	if hasDeadline {
		if remaining := time.Until(deadline); remaining < interval {
			interval = remaining
			if interval <= 0 {
				return context.DeadlineExceeded
			}
		}
	}
	t := time.NewTimer(interval)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
