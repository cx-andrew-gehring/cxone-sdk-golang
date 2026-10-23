package policies_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/policies"
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

func TestList(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"policies":[{"id":"pol1","name":"Security Policy"}],"filteredPoliciesCount":1}`)
	})
	resp, err := policies.List(context.Background(), exec, base, models.PolicyFilter{Limit: 10, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Policies) != 1 || resp.Policies[0].Name != "Security Policy" {
		t.Errorf("policies = %+v", resp.Policies)
	}
	if !strings.Contains(gotQuery, "limit=10") || !strings.Contains(gotQuery, "page=1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestCountFiltered(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"policies":[],"filteredPoliciesCount":5}`)
	})
	n, err := policies.CountFiltered(context.Background(), exec, base, models.PolicyFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("count = %d", n)
	}
}

func TestListViolations(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"incidents":[{"violationId":1,"policyName":"Sec Policy","projectName":"proj1"}],"filteredIncidentsCount":1}`)
	})
	resp, err := policies.ListViolations(context.Background(), exec, base, models.PolicyViolationFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Incidents) != 1 || resp.Incidents[0].PolicyName != "Sec Policy" {
		t.Errorf("incidents = %+v", resp.Incidents)
	}
}

func TestGetViolationDetails(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"astProjectId":"p1","scanId":"s1","status":"Passed","breakBuild":false}`)
	})
	det, err := policies.GetViolationDetails(context.Background(), exec, base, "p1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if det.Status != "Passed" {
		t.Errorf("status = %q", det.Status)
	}
	if !strings.Contains(gotQuery, "astProjectId=p1") || !strings.Contains(gotQuery, "scanId=s1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestAssignToProjects(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	err := policies.AssignToProjects(context.Background(), exec, base, "pol1", []models.ProjectResponseModel{
		{ID: "p1", Name: "proj1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"id":"pol1"`) || !strings.Contains(gotBody, `"AstProjectId":"p1"`) {
		t.Errorf("body = %q", gotBody)
	}
}
