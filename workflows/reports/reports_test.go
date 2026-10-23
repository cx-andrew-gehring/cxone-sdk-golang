package reports_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/reports"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *reports.Backend {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    srv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test",
		CorrelationID: "corr",
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	return &reports.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestJSONReportHappyPath(t *testing.T) {
	var pollHits int32
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/reports/json":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"reportId":"r1"}`)
		case "/api/reports/json/r1":
			n := atomic.AddInt32(&pollHits, 1)
			if n < 2 {
				_, _ = io.WriteString(w, `{"reportId":"r1","status":"started"}`)
				return
			}
			_, _ = io.WriteString(w, `{"reportId":"r1","status":"completed","url":"https://x/r1.json"}`)
		case "/api/reports/json/r1/download":
			_, _ = io.WriteString(w, `{"results":[]}`)
		default:
			t.Errorf("unexpected: %s", r.URL.Path)
		}
	})
	h, err := reports.NewJSONReport(c).
		ForScan("s1", "p1", "main").
		WithSections("scan-summary", "results").
		WithScanners("sast").
		Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.ReportID() != "r1" {
		t.Errorf("reportID = %q", h.ReportID())
	}
	var buf bytes.Buffer
	n, err := h.WaitAndDownload(context.Background(), &buf, reports.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || buf.Len() == 0 {
		t.Errorf("download was empty")
	}
	if h.Status() != "completed" {
		t.Errorf("status = %q", h.Status())
	}
	if h.URL() != "https://x/r1.json" {
		t.Errorf("URL = %q", h.URL())
	}
}

func TestReportFailedStatusReturnsReportError(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/reports/json":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"reportId":"r1"}`)
		case "/api/reports/json/r1":
			_, _ = io.WriteString(w, `{"reportId":"r1","status":"failed"}`)
		}
	})
	h, err := reports.NewJSONReport(c).
		ForScan("s1", "p1", "main").
		Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = h.Wait(context.Background(), reports.WaitOptions{PollInterval: 5 * time.Millisecond})
	var rerr *cxerrors.ReportError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *ReportError, got %T: %v", err, err)
	}
	if rerr.ReportID != "r1" {
		t.Errorf("reportID = %q", rerr.ReportID)
	}
}

func TestReportBuilderRequiresScanID(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := reports.NewJSONReport(c).Generate(context.Background())
	var ce *cxerrors.ConfigurationError
	if !errors.As(err, &ce) || ce.Field != "ScanID" {
		t.Errorf("got %v", err)
	}
}

func TestPDFReportBuilderUsesPDFEndpoints(t *testing.T) {
	var hitJSON, hitPDF bool
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/reports/json":
			hitJSON = true
		case "/api/reports/pdf":
			hitPDF = true
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"reportId":"r1"}`)
		}
	})
	_, err := reports.NewPDFReport(c).
		ForScan("s1", "p1", "main").
		Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hitJSON {
		t.Errorf("PDF builder hit JSON endpoint")
	}
	if !hitPDF {
		t.Errorf("PDF endpoint not called")
	}
}

func TestReportWaitMaxDurationDeadline(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/reports/json":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"reportId":"r1"}`)
		case "/api/reports/json/r1":
			_, _ = io.WriteString(w, `{"reportId":"r1","status":"started"}`)
		}
	})
	h, err := reports.NewJSONReport(c).ForScan("s1", "p1", "main").Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = h.Wait(context.Background(), reports.WaitOptions{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  50 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Errorf("waiter overshot deadline: %v", time.Since(start))
	}
}
