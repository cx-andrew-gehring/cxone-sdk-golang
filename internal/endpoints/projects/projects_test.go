package projects_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/projects"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// stubAuth always hands out the same token; mirrors the one in
// internal/transport tests but redefined here to keep this package
// dependency-free.
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

func TestCreateProject(t *testing.T) {
	var gotMethod, gotPath, gotCT, gotBody string
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"p1","name":"acme","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","groups":["g1"],"tags":{"env":"prod"},"repoUrl":"git@x","mainBranch":"main","applicationIds":["a1"]}`)
	})

	out, err := projects.Create(context.Background(), exec, base, &models.Project{
		Name:    "acme",
		RepoURL: "git@x",
		Tags:    map[string]string{"env": "prod"},
		Groups:  []string{"g1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "p1" || out.Name != "acme" {
		t.Errorf("unexpected response: %+v", out)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if gotPath != "/api/projects" {
		t.Errorf("path = %q", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if !strings.Contains(gotBody, `"name":"acme"`) {
		t.Errorf("body missing name: %q", gotBody)
	}
}

func TestListProjectsWithQuery(t *testing.T) {
	var gotQuery url.Values
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"totalCount":2,"filteredTotalCount":2,"projects":[{"id":"p1","name":"a"},{"id":"p2","name":"b"}]}`)
	})
	out, err := projects.List(context.Background(), exec, base, url.Values{
		"limit":  {"50"},
		"offset": {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalCount != 2 || len(out.Projects) != 2 {
		t.Errorf("got %+v", out)
	}
	if gotQuery.Get("limit") != "50" || gotQuery.Get("offset") != "0" {
		t.Errorf("query = %v", gotQuery)
	}
}

func TestGetProjectByID(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/p1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"p1","name":"a"}`)
	})
	out, err := projects.Get(context.Background(), exec, base, "p1")
	if err != nil || out.ID != "p1" {
		t.Fatalf("err=%v out=%+v", err, out)
	}
}

func TestGetProjectNotFoundReturnsResponseError(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"project not found","code":404}`)
	})
	_, err := projects.Get(context.Background(), exec, base, "nope")
	var rerr *cxerrors.ResponseError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *ResponseError, got %T: %v", err, err)
	}
	if rerr.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", rerr.StatusCode)
	}
	if !strings.Contains(rerr.Reason, "project not found") {
		t.Errorf("reason = %q", rerr.Reason)
	}
}

func TestUpdateProject(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/projects/p1" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := projects.Update(context.Background(), exec, base, "p1", &models.Project{Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteProject(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/projects/p1" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := projects.Delete(context.Background(), exec, base, "p1"); err != nil {
		t.Fatal(err)
	}
}

func TestGetBranchesForcesProjectIDQuery(t *testing.T) {
	var gotQuery url.Values
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/branches" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `["main","develop"]`)
	})
	branches, err := projects.GetBranches(context.Background(), exec, base, "p1", url.Values{"limit": {"100"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[0] != "main" {
		t.Errorf("branches = %v", branches)
	}
	if got := gotQuery.Get("project-id"); got != "p1" {
		t.Errorf("project-id = %q", got)
	}
	if got := gotQuery.Get("limit"); got != "100" {
		t.Errorf("limit = %q", got)
	}
}

func TestProjectTags(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/tags" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"env":["prod","dev"],"team":["sec"]}`)
	})
	tags, err := projects.Tags(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags["env"]) != 2 || tags["team"][0] != "sec" {
		t.Errorf("tags = %v", tags)
	}
}

func TestMoveApplications(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	err := projects.MoveApplications(context.Background(), exec, base, "p1", []string{"app-old"}, []string{"app-new"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q", gotMethod)
	}
	if gotPath != "/api/projects/reassign/p1" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotBody, `"applicationIdsToDisassociate":["app-old"]`) {
		t.Errorf("body = %q", gotBody)
	}
	if !strings.Contains(gotBody, `"applicationIdsToAssociate":["app-new"]`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestMoveApplicationsValidation(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	err := projects.MoveApplications(context.Background(), exec, base, "", nil, nil)
	if err == nil {
		t.Error("expected error for empty projectID")
	}
}

func TestUpdateProjectConfigurationKebabCaseQuery(t *testing.T) {
	var gotQuery url.Values
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/configuration/project" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := []models.ProjectConfiguration{
		{Key: "scan.config.sast.presetName", Value: "ASA Premium", ValueType: "String"},
	}
	if err := projects.UpdateConfiguration(context.Background(), exec, base, "p1", cfg); err != nil {
		t.Fatal(err)
	}
	if got := gotQuery.Get("project-id"); got != "p1" {
		t.Errorf("project-id = %q", got)
	}
}
