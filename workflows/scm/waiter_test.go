package scm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scm"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *scm.Backend {
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
	return &scm.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestFromProcessID(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("processId"); got != "p1" {
			t.Errorf("processId = %q", got)
		}
		_, _ = io.WriteString(w, `{"migrationStatus":"IN_PROGRESS","totalProjects":1}`)
	})
	insp, err := scm.FromProcessID(context.Background(), b, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if insp.ID() != "p1" {
		t.Errorf("id = %q", insp.ID())
	}
	if insp.Terminal() {
		t.Error("IN_PROGRESS should not be terminal")
	}
	if !insp.Executing() {
		t.Error("IN_PROGRESS should be executing")
	}
}

func TestFromProcessIDValidation(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	if _, err := scm.FromProcessID(context.Background(), b, ""); err == nil {
		t.Error("expected error for empty processID")
	}
	if _, err := scm.FromProcessID(context.Background(), nil, "p1"); err == nil {
		t.Error("expected error for nil backend")
	}
}

func TestWaitUntilCompleteHappyPath(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) <= 2 {
			_, _ = io.WriteString(w, `{"migrationStatus":"IN_PROGRESS"}`)
		} else {
			_, _ = io.WriteString(w, `{"migrationStatus":"OK","migratedProjects":1,"totalProjects":1}`)
		}
	})
	insp, _ := scm.FromProcessID(context.Background(), b, "p1")
	final, err := scm.WaitUntilComplete(context.Background(), insp, scm.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Completed() {
		t.Errorf("expected completed, got %q", final.Status())
	}
}

func TestWaitUntilCompleteFailedStatus(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"migrationStatus":"FAILED","summary":"boom"}`)
	})
	insp, _ := scm.FromProcessID(context.Background(), b, "p1")
	_, err := scm.WaitUntilComplete(context.Background(), insp, scm.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error for failed conversion")
	}
	if !strings.Contains(err.Error(), "FAILED") {
		t.Errorf("error should name the status: %q", err)
	}
}

// The API reports a partially-successful batch with an overall status of
// "OK". For a caller converting a single project, that project landing in
// failedProjectList is an outright failure.
func TestWaitUntilCompleteOKWithFailedProjectsIsFailure(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"migrationStatus":"OK",
			"totalProjects":2,
			"migratedProjects":1,
			"successfulProjectsList":["acme/one"],
			"failedProjectList":["acme/two"]
		}`)
	})
	insp, _ := scm.FromProcessID(context.Background(), b, "p1")
	final, err := scm.WaitUntilComplete(context.Background(), insp, scm.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error when a project failed despite status OK")
	}
	if !strings.Contains(err.Error(), "acme/two") {
		t.Errorf("error should name the failed repo: %q", err)
	}
	if final.Completed() {
		t.Error("must not report Completed when a project failed")
	}
	if !final.Failed() {
		t.Error("expected Failed")
	}
}

// An unrecognized status must never be treated as terminal — a false success
// is worse than a stall.
func TestWaitUntilCompleteUnknownStatusStalls(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"migrationStatus":"SOMETHING_NEW"}`)
	})
	insp, _ := scm.FromProcessID(context.Background(), b, "p1")
	if insp.Terminal() {
		t.Fatal("unknown status must not be terminal")
	}
	_, err := scm.WaitUntilComplete(context.Background(), insp, scm.WaitOptions{
		PollInterval: 5 * time.Millisecond,
		MaxDuration:  30 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected deadline error rather than a success verdict")
	}
}

func TestWaitUntilCompleteCancellation(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"migrationStatus":"IN_PROGRESS"}`)
	})
	insp, _ := scm.FromProcessID(context.Background(), b, "p1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := scm.WaitUntilComplete(ctx, insp, scm.WaitOptions{
		PollInterval: 10 * time.Millisecond,
	}); err == nil {
		t.Fatal("expected context error")
	}
}

func TestWaitUntilCompleteNilInspector(t *testing.T) {
	if _, err := scm.WaitUntilComplete(context.Background(), nil, scm.WaitOptions{}); err == nil {
		t.Error("expected error for nil inspector")
	}
}
