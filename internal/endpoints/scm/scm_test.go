package scm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scm"
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

// noServer returns an executor whose handler fails the test if reached. Used
// by the validation tests, which must all short-circuit before any HTTP call.
func noServer(t *testing.T) (*transport.Executor, string) {
	t.Helper()
	return newExec(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("should not reach server: %s %s", r.Method, r.URL.Path)
	})
}

// validRequest is a minimal request that passes validation; individual tests
// mutate one field to assert the corresponding check.
func validRequest() *models.SCMConversionRequest {
	return &models.SCMConversionRequest{
		SCMType:     models.SCMTypeGitHub,
		OrgIdentity: "acme",
		Projects: []models.SCMConversionProject{{
			CxProjectID:       "proj-1",
			SCMRepositoryURL:  "https://github.com/acme/repo",
			ProtectedBranches: []string{"main"},
		}},
	}
}

func TestDisconnectProject(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	if err := scm.DisconnectProject(context.Background(), exec, base, "proj-1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/api/repos-manager/projects/proj-1/disconnect"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

// The disconnect endpoint may answer 200 with a body or 204 with none; both
// must be accepted, and neither may be decoded into anything.
func TestDisconnectProjectAcceptsOKWithBody(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"message":"disconnected"}`)
	})
	if err := scm.DisconnectProject(context.Background(), exec, base, "proj-1"); err != nil {
		t.Fatal(err)
	}
}

func TestDisconnectProjectValidation(t *testing.T) {
	exec, base := noServer(t)
	if err := scm.DisconnectProject(context.Background(), exec, base, ""); err == nil {
		t.Error("expected error for empty projectID")
	}
}

func TestConvertProjects(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"processId":"XyGB7ZVneh4","message":"Started to convert projects."}`)
	})

	req := validRequest()
	req.Token = "super-secret-pat"
	out, err := scm.ConvertProjects(context.Background(), exec, base, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.ProcessID != "XyGB7ZVneh4" {
		t.Errorf("processId = %q", out.ProcessID)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/api/repos-manager/project-conversion"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	// The token must reach the server verbatim — redaction applies to logs
	// and errors, never to the wire.
	if !strings.Contains(gotBody, "super-secret-pat") {
		t.Errorf("token missing from request body: %s", gotBody)
	}
	// Process-level booleans must serialize even when false, otherwise the
	// caller cannot express "do not auto-scan".
	for _, field := range []string{`"webhookEnabled":false`, `"autoScanCxProjectAfterConversion":false`} {
		if !strings.Contains(gotBody, field) {
			t.Errorf("expected %s in body: %s", field, gotBody)
		}
	}
}

// Per-project booleans are pointers so that nil means "inherit the
// process-level value" rather than "false".
func TestConvertProjectsOmitsUnsetProjectOverrides(t *testing.T) {
	var gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"processId":"p1"}`)
	})

	req := validRequest()
	req.WebhookEnabled = true
	if _, err := scm.ConvertProjects(context.Background(), exec, base, req); err != nil {
		t.Fatal(err)
	}

	projectsPart := gotBody[strings.Index(gotBody, `"projects"`):]
	if strings.Contains(projectsPart, "webhookEnabled") {
		t.Errorf("unset per-project override should be omitted: %s", projectsPart)
	}

	// An explicit false must survive.
	off := false
	req.Projects[0].WebhookEnabled = &off
	if _, err := scm.ConvertProjects(context.Background(), exec, base, req); err != nil {
		t.Fatal(err)
	}
	projectsPart = gotBody[strings.Index(gotBody, `"projects"`):]
	if !strings.Contains(projectsPart, `"webhookEnabled":false`) {
		t.Errorf("explicit false override should be sent: %s", projectsPart)
	}
}

func TestConvertProjectsValidation(t *testing.T) {
	autoScanNoBranch := validRequest()
	autoScanNoBranch.AutoScanCxProjectAfterConversion = true

	noBranches := validRequest()
	noBranches.Projects[0].ProtectedBranches = nil

	badType := validRequest()
	badType.SCMType = "githubb"

	noOrg := validRequest()
	noOrg.OrgIdentity = ""

	noProjects := validRequest()
	noProjects.Projects = nil

	cases := []struct {
		name string
		req  *models.SCMConversionRequest
	}{
		{"nil request", nil},
		{"missing scmType", &models.SCMConversionRequest{OrgIdentity: "acme"}},
		{"unknown scmType", badType},
		{"missing orgIdentity", noOrg},
		{"no projects", noProjects},
		{"no protected branches", noBranches},
		{"auto scan without branch", autoScanNoBranch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec, base := noServer(t)
			if _, err := scm.ConvertProjects(context.Background(), exec, base, tc.req); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestGetConversionStatus(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = io.WriteString(w, `{
			"migrationStatus":"OK",
			"summary":"Finished converting projects",
			"totalProjects":2,
			"migratedProjects":2,
			"successfulProjectsList":["acme/one","acme/two"],
			"failedProjectList":[]
		}`)
	})
	out, err := scm.GetConversionStatus(context.Background(), exec, base, "XyGB7ZVneh4")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if want := "/api/repos-manager/project-conversion"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if want := "processId=XyGB7ZVneh4"; gotQuery != want {
		t.Errorf("query = %q, want %q", gotQuery, want)
	}
	if out.Status != "OK" || out.MigratedProjects != 2 {
		t.Errorf("got %+v", out)
	}
	if len(out.SuccessfulProjectsList) != 2 {
		t.Errorf("successful list = %v", out.SuccessfulProjectsList)
	}
}

func TestGetConversionStatusValidation(t *testing.T) {
	exec, base := noServer(t)
	if _, err := scm.GetConversionStatus(context.Background(), exec, base, ""); err == nil {
		t.Error("expected error for empty processID")
	}
}
