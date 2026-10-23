package audit_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/audit"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newExec(t *testing.T, h http.HandlerFunc) (*transport.Executor, string) {
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
	return exec, srv.URL + "/"
}

func TestCreateSession(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"id":"sess-1","data":{"status":"ALLOCATED","requestId":"req-1"}}`)
	})
	sess, err := audit.CreateSession(context.Background(), exec, base, &models.AuditCreateRequest{
		Scanner: "sast",
		Filter:  "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"scanner":"sast"`) {
		t.Errorf("body = %q", gotBody)
	}
	if sess.ID != "sess-1" || sess.Data.Status != "ALLOCATED" {
		t.Errorf("session = %+v", sess)
	}
	if sess.Engine != "sast" {
		t.Errorf("Engine = %q (should be set from request)", sess.Engine)
	}
}

func TestDeleteSession(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := audit.DeleteSession(context.Background(), exec, base, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotPath, "sessions/sess-1") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestKeepAlive(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := audit.KeepAlive(context.Background(), exec, base, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestGetRequestStatus(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "requests/req-1") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"completed":true,"value":["go","java"],"status":"Completed"}`)
	})
	status, err := audit.GetRequestStatus(context.Background(), exec, base, "sess-1", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Completed || status.Status != "Completed" {
		t.Errorf("status = %+v", status)
	}
}

func TestGetScanSources(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sources") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"filename":"main.go","language":"go","size":1024}]`)
	})
	files, err := audit.GetScanSources(context.Background(), exec, base, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Filename != "main.go" {
		t.Errorf("files = %+v", files)
	}
}

func TestRunScan(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	err := audit.RunScan(context.Background(), exec, base, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/sources/scan") {
		t.Errorf("path = %q", gotPath)
	}
}
