package scm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/scm"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
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

func TestListIntegrations(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":1,"type":"github","displayName":"GitHub Cloud","repoCount":42}]`)
	})
	out, err := scm.ListIntegrations(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].DisplayName != "GitHub Cloud" {
		t.Errorf("got %+v", out)
	}
	if !strings.Contains(gotQuery, "fields=repoCount") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetRepository(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/repo/123") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"repo-uuid","url":"https://github.com/org/repo","webhookEnabled":true}`)
	})
	repo, err := scm.GetRepository(context.Background(), exec, base, 123)
	if err != nil {
		t.Fatal(err)
	}
	if repo.URL != "https://github.com/org/repo" {
		t.Errorf("url = %q", repo.URL)
	}
	if !repo.WebhookEnabled {
		t.Error("expected webhook enabled")
	}
}

func TestGetRepositoryValidation(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	_, err := scm.GetRepository(context.Background(), exec, base, 0)
	if err == nil {
		t.Error("expected error for zero repositoryID")
	}
}

func TestDisconnectProject(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if !strings.HasSuffix(r.URL.Path, "/projects/proj-cx-sunny007/disconnect") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})
	err := scm.DisconnectProject(context.Background(), exec, base, "proj-cx-sunny007")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
}

func TestDisconnectProjectValidation(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	err := scm.DisconnectProject(context.Background(), exec, base, "")
	if err == nil {
		t.Error("expected error for empty projectID")
	}
}