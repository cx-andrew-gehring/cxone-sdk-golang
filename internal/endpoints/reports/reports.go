// Package reports implements the Checkmarx One Reports API (low-level layer).
//
// Generating a report is a three-step dance:
//
//  1. POST /api/reports/json (or /pdf) with the request body → 202 Accepted
//     returns a reportId.
//  2. Poll GET /api/reports/json/{id}?return-url=true until Status is
//     "completed" or "failed".
//  3. When complete, the polling response carries a URL the caller can
//     follow to download the file. Alternatively the dedicated
//     /api/reports/json/{id}/download endpoint streams the file directly.
//
// The high-level workflow layer (cxone/workflows/reports) wraps this whole
// dance behind a fluent builder + waiter; this package provides the
// underlying primitives.
package reports

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// JSONPath / PDFPath are the report-format roots under /api/reports.
const (
	JSONPath = "api/reports/json"
	PDFPath  = "api/reports/pdf"
)

// CreateJSON queues a JSON report. POST /api/reports/json → 202.
func CreateJSON(ctx context.Context, e *transport.Executor, baseURL string, in *models.JSONReportRequest) (*models.JSONReportResponse, error) {
	var out models.JSONReportResponse
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, JSONPath),
		nil, in, []int{http.StatusAccepted}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollJSON returns the current status of a queued JSON report.
// GET /api/reports/json/{reportId}?return-url=true → 200.
func PollJSON(ctx context.Context, e *transport.Executor, baseURL, reportID string) (*models.JSONReportPollingResponse, error) {
	var out models.JSONReportPollingResponse
	q := url.Values{"return-url": {"true"}}
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, JSONPath+"/"+reportID),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadJSON streams the completed JSON report body. The caller MUST close
// the returned ReadCloser. GET /api/reports/json/{reportId}/download → 200.
func DownloadJSON(ctx context.Context, e *transport.Executor, baseURL, reportID string) (io.ReadCloser, error) {
	return transport.DoStream(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, JSONPath+"/"+reportID+"/download"),
		nil, []int{http.StatusOK})
}

// CreatePDF queues a PDF report. POST /api/reports/pdf → 202.
func CreatePDF(ctx context.Context, e *transport.Executor, baseURL string, in *models.PDFReportRequest) (*models.PDFReportResponse, error) {
	var out models.PDFReportResponse
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, PDFPath),
		nil, in, []int{http.StatusAccepted}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollPDF returns the current status of a queued PDF report.
// GET /api/reports/pdf/{reportId}?return-url=true → 200.
func PollPDF(ctx context.Context, e *transport.Executor, baseURL, reportID string) (*models.PDFReportPollingResponse, error) {
	var out models.PDFReportPollingResponse
	q := url.Values{"return-url": {"true"}}
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, PDFPath+"/"+reportID),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadPDF streams the completed PDF report body. The caller MUST close
// the returned ReadCloser. GET /api/reports/pdf/{reportId}/download → 200.
func DownloadPDF(ctx context.Context, e *transport.Executor, baseURL, reportID string) (io.ReadCloser, error) {
	return transport.DoStream(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, PDFPath+"/"+reportID+"/download"),
		nil, []int{http.StatusOK})
}
