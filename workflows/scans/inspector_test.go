package scans_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *scans.Backend {
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
	return &scans.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestFromScanIDPopulatesInspector(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"s1","status":"Running","branch":"main","engines":["sast"]}`)
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if insp.ID() != "s1" {
		t.Errorf("id = %q", insp.ID())
	}
	if !insp.Executing() || insp.Successful() || insp.Failed() {
		t.Errorf("Running scan should be Executing only: exec=%v ok=%v fail=%v",
			insp.Executing(), insp.Successful(), insp.Failed())
	}
	if got := insp.StateMessage(); got != "Running" {
		t.Errorf("StateMessage = %q", got)
	}
}

func TestInspectorClassifiesPlainStates(t *testing.T) {
	cases := []struct {
		status        string
		executing, ok, failed bool
	}{
		{models.ScanQueued, true, false, false},
		{models.ScanRunning, true, false, false},
		{models.ScanCompleted, false, true, false},
		{models.ScanFailed, false, false, true},
		{models.ScanCanceled, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			insp, _ := scans.FromScanModel(&scans.Backend{Executor: nopExecutor()}, &models.ScanResponseModel{
				Status: models.ScanStatus(tc.status),
			})
			if insp.Executing() != tc.executing || insp.Successful() != tc.ok || insp.Failed() != tc.failed {
				t.Errorf("status=%s: exec=%v ok=%v fail=%v, want %v/%v/%v",
					tc.status, insp.Executing(), insp.Successful(), insp.Failed(),
					tc.executing, tc.ok, tc.failed)
			}
		})
	}
}

func TestInspectorPartialAllEnginesCompleteIsSuccessful(t *testing.T) {
	insp, _ := scans.FromScanModel(&scans.Backend{Executor: nopExecutor()}, &models.ScanResponseModel{
		Status:  models.ScanPartial,
		Engines: []string{"sast", "sca"},
		StatusDetails: []models.StatusInfo{
			{Name: "sast", Status: models.ScanCompleted},
			{Name: "sca", Status: models.ScanCompleted},
		},
	})
	if !insp.Successful() {
		t.Errorf("Partial+all-completed should be Successful")
	}
	if insp.Executing() || insp.Failed() {
		t.Errorf("flags inconsistent: exec=%v fail=%v", insp.Executing(), insp.Failed())
	}
}

func TestInspectorPartialOneEngineFailedIsFailed(t *testing.T) {
	insp, _ := scans.FromScanModel(&scans.Backend{Executor: nopExecutor()}, &models.ScanResponseModel{
		Status:  models.ScanPartial,
		Engines: []string{"sast", "sca"},
		StatusDetails: []models.StatusInfo{
			{Name: "sast", Status: models.ScanCompleted},
			{Name: "sca", Status: models.ScanFailed, Details: "engine timed out"},
		},
	})
	if !insp.Failed() {
		t.Errorf("Partial+one-failed should be Failed")
	}
	if insp.Successful() || insp.Executing() {
		t.Errorf("flags inconsistent")
	}
	if got := insp.StateMessage(); got == models.ScanPartial || got == "" {
		t.Errorf("StateMessage should include per-engine details, got %q", got)
	}
}

func TestInspectorPartialOneEngineRunningIsExecuting(t *testing.T) {
	insp, _ := scans.FromScanModel(&scans.Backend{Executor: nopExecutor()}, &models.ScanResponseModel{
		Status:  models.ScanPartial,
		Engines: []string{"sast", "sca", "kics"},
		StatusDetails: []models.StatusInfo{
			{Name: "sast", Status: models.ScanCompleted},
			{Name: "sca", Status: models.ScanCompleted},
			{Name: "kics", Status: models.ScanRunning},
		},
	})
	if !insp.Executing() {
		t.Errorf("Partial+one-running should be Executing")
	}
	if insp.Successful() || insp.Failed() {
		t.Errorf("flags inconsistent")
	}
}

func TestInspectorPartialIgnoresUnrequestedEngines(t *testing.T) {
	insp, _ := scans.FromScanModel(&scans.Backend{Executor: nopExecutor()}, &models.ScanResponseModel{
		Status:  models.ScanPartial,
		Engines: []string{"sast"}, // only sast was requested
		StatusDetails: []models.StatusInfo{
			{Name: "sast", Status: models.ScanCompleted},
			// platform-injected entries that the original request didn't ask for:
			{Name: "auto-discovery", Status: models.ScanFailed},
		},
	})
	if !insp.Successful() {
		t.Errorf("Unrequested engine failure should be ignored: exec=%v ok=%v fail=%v",
			insp.Executing(), insp.Successful(), insp.Failed())
	}
}

func TestInspectorPartialNoMatchingDetailsIsFailedConservatively(t *testing.T) {
	insp, _ := scans.FromScanModel(&scans.Backend{Executor: nopExecutor()}, &models.ScanResponseModel{
		Status:  models.ScanPartial,
		Engines: []string{"sast"},
		StatusDetails: []models.StatusInfo{
			{Name: "kics", Status: models.ScanCompleted}, // unrelated
		},
	})
	if !insp.Failed() {
		t.Errorf("Partial with no matching engine details should be Failed (conservative)")
	}
}

func TestRefreshUpdatesCachedScan(t *testing.T) {
	gen := 0
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gen++
		if gen == 1 {
			_, _ = io.WriteString(w, `{"id":"s1","status":"Running"}`)
		} else {
			_, _ = io.WriteString(w, `{"id":"s1","status":"Completed"}`)
		}
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !insp.Executing() {
		t.Errorf("first fetch should be Running")
	}
	if _, err := insp.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !insp.Successful() {
		t.Errorf("after refresh: should be Successful, got status=%q", insp.Status())
	}
}

// nopExecutor returns a transport.Executor pointed at an unused server. Used
// in tests that build inspectors via FromScanModel — those don't issue HTTP,
// but the inspector still requires a non-nil Backend.Executor.
func nopExecutor() *transport.Executor {
	return transport.NewExecutor(transport.Config{
		HTTPClient:    http.DefaultClient,
		Authenticator: stubAuth{},
		RetryPolicy:   retry.Policy{MaxAttempts: 1},
	})
}
