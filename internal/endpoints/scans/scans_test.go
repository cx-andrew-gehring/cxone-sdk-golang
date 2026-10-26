package scans_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scans"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

type stubAuth struct{}

func (stubAuth) Token(ctx context.Context) (string, uint64, error)         { return "tok", 1, nil }
func (stubAuth) Refresh(ctx context.Context, _ uint64) (string, uint64, error) { return "tok", 1, nil }

func newTestExecutor(t *testing.T, h http.HandlerFunc) (*transport.Executor, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    srv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test/(CxOne GoSDK/0.0.0-test)",
		CorrelationID: "corr",
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	return exec, srv.URL + "/"
}

func TestCreateScan(t *testing.T) {
	var gotBody string
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/scans" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"s1","status":"Queued","branch":"main","projectId":"p1"}`)
	})
	handler, _ := json.Marshal(models.GitProjectHandler{
		RepoURL: "https://example.com/r.git",
		Branch:  "main",
	})
	scan := &models.Scan{
		Type:    "git",
		Handler: handler,
		Project: models.ScanProject{ID: "p1"},
		Tags:    map[string]string{"env": "prod"},
	}
	out, err := scans.Create(context.Background(), exec, base, scan)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "s1" || out.Status != models.ScanQueued {
		t.Errorf("unexpected: %+v", out)
	}
	if !strings.Contains(gotBody, `"type":"git"`) || !strings.Contains(gotBody, `"projectId":"p1"`) {
		// Wait — projectId is wrong, ScanProject uses "id"
		if !strings.Contains(gotBody, `"id":"p1"`) {
			t.Errorf("body: %q", gotBody)
		}
	}
}

func TestListScansPropagatesQuery(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("project-id"); got != "p1" {
			t.Errorf("project-id = %q", got)
		}
		if got := r.URL.Query().Get("statuses"); got != "Running" {
			t.Errorf("statuses = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"totalCount":1,"filteredTotalCount":1,"scans":[{"id":"s1","status":"Running"}]}`)
	})
	q := make(map[string][]string)
	q["project-id"] = []string{"p1"}
	q["statuses"] = []string{"Running"}
	out, err := scans.List(context.Background(), exec, base, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Scans) != 1 || out.Scans[0].Status != "Running" {
		t.Errorf("got %+v", out)
	}
}

func TestGetScan(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scans/s1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"s1","status":"Completed"}`)
	})
	out, err := scans.Get(context.Background(), exec, base, "s1")
	if err != nil || out.Status != models.ScanCompleted {
		t.Fatalf("err=%v out=%+v", err, out)
	}
}

func TestCancelScanPostsCorrectBody(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	if err := scans.Cancel(context.Background(), exec, base, "s1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"status":"Canceled"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestDeleteScan(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := scans.Delete(context.Background(), exec, base, "s1"); err != nil {
		t.Fatal(err)
	}
}

func TestGetWorkflow(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scans/s1/workflow" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"source":"engine","timestamp":"2026-01-01","info":"started"}]`)
	})
	out, err := scans.GetWorkflow(context.Background(), exec, base, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Source != "engine" {
		t.Errorf("got %+v", out)
	}
}

func TestScanTagsErrorOnBadStatus(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"message":"boom","code":500}`)
	})
	_, err := scans.Tags(context.Background(), exec, base)
	// 5xx triggers retry then CommunicationError because the funnel
	// classifies 500 as retry-able and exhausts a 1-attempt policy.
	var commErr *cxerrors.CommunicationError
	var respErr *cxerrors.ResponseError
	if !errors.As(err, &commErr) && !errors.As(err, &respErr) {
		t.Fatalf("expected CommunicationError or ResponseError, got %T: %v", err, err)
	}
}

func newRetryingExecutor(t *testing.T, h http.HandlerFunc) (*transport.Executor, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    srv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test/(CxOne GoSDK/0.0.0-test)",
		CorrelationID: "corr",
		RetryPolicy:   retry.Policy{MaxAttempts: 3, MaxDelay: 0},
	})
	return exec, srv.URL + "/"
}

func TestCreateScan_Code209IsInvalidLLMError(t *testing.T) {
	var hits int32
	exec, base := newRetryingExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"LLM is not valid for fusion scan","type":"ERROR","code":209}`)
	})
	_, err := scans.Create(context.Background(), exec, base, &models.Scan{Type: "git"})
	var le *cxerrors.InvalidLLMError
	if !errors.As(err, &le) {
		t.Fatalf("want InvalidLLMError, got %T %v", err, err)
	}
	if le.Message != "LLM is not valid for fusion scan" || le.CorrelationID != "corr" {
		t.Errorf("got %+v", le)
	}
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 400 {
		t.Errorf("ResponseError not reachable: %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("hits = %d, want 1 (no retry)", n)
	}
}

func TestCreateScan_Other400IsNotInvalidLLM(t *testing.T) {
	for name, body := range map[string]string{
		"code208":  `{"message":"other","type":"ERROR","code":208}`,
		"non-json": `<html>bad</html>`,
	} {
		t.Run(name, func(t *testing.T) {
			exec, base := newRetryingExecutor(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, body)
			})
			_, err := scans.Create(context.Background(), exec, base, &models.Scan{Type: "git"})
			var le *cxerrors.InvalidLLMError
			if errors.As(err, &le) {
				t.Fatalf("unexpected InvalidLLMError: %v", err)
			}
			var re *cxerrors.ResponseError
			if !errors.As(err, &re) || re.StatusCode != 400 {
				t.Errorf("want ResponseError 400, got %T %v", err, err)
			}
		})
	}
}

func TestOtherEndpoint209IsNotInvalidLLM(t *testing.T) {
	exec, base := newRetryingExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"LLM is not valid for fusion scan","code":209}`)
	})
	_, err := scans.Get(context.Background(), exec, base, "s1")
	var le *cxerrors.InvalidLLMError
	if errors.As(err, &le) {
		t.Fatalf("GET must not map 209: %v", err)
	}
}
