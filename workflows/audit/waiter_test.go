package audit_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/audit"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *audit.Backend {
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
	return &audit.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestWaitForRequestHappyPath(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if n <= 2 {
			_, _ = io.WriteString(w, `{"completed":false,"status":"Running"}`)
		} else {
			_, _ = io.WriteString(w, `{"completed":true,"value":["go","java"],"status":"Completed"}`)
		}
	})
	session := &models.AuditSession{ID: "sess-1", Data: models.AuditSessionData{Status: "ALLOCATED", RequestID: "req-1"}}
	insp, _ := audit.FromSession(b, session)

	val, err := audit.WaitForRequest(context.Background(), insp, "req-1", audit.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if val == nil {
		t.Fatal("expected non-nil value")
	}
}

func TestWaitForRequestFailure(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, `{"completed":false,"status":"Failed","value":"engine crash"}`)
	})
	session := &models.AuditSession{ID: "sess-1"}
	insp, _ := audit.FromSession(b, session)

	_, err := audit.WaitForRequest(context.Background(), insp, "req-1", audit.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error for failed request")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error = %q, want 'failed'", err)
	}
}

func TestWaitForRequestCancellation(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, `{"completed":false,"status":"Running"}`)
	})
	session := &models.AuditSession{ID: "sess-1"}
	insp, _ := audit.FromSession(b, session)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := audit.WaitForRequest(ctx, insp, "req-1", audit.WaitOptions{
		PollInterval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestWaitForRequestErrorCode(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, `{"completed":false,"code":500,"message":"internal error"}`)
	})
	session := &models.AuditSession{ID: "sess-1"}
	insp, _ := audit.FromSession(b, session)

	_, err := audit.WaitForRequest(context.Background(), insp, "req-1", audit.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error for error code")
	}
	if !strings.Contains(err.Error(), "internal error") {
		t.Errorf("error = %q", err)
	}
}

func TestSessionInspectorKeepAliveAndDelete(t *testing.T) {
	var keepAlives, deletes int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			atomic.AddInt32(&keepAlives, 1)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			atomic.AddInt32(&deletes, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	session := &models.AuditSession{ID: "sess-1", Data: models.AuditSessionData{Status: "ALLOCATED"}}
	insp, _ := audit.FromSession(b, session)

	if err := insp.KeepAlive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := insp.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&keepAlives) != 1 {
		t.Error("expected 1 keep-alive")
	}
	if atomic.LoadInt32(&deletes) != 1 {
		t.Error("expected 1 delete")
	}
}

// Suppress unused import warning for fmt.
var _ = fmt.Sprint
