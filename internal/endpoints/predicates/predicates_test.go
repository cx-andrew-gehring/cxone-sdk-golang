package predicates_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/predicates"
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

func TestGetSAST(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"predicateHistoryPerProject":[],"totalCount":0}`)
	})
	_, err := predicates.GetSAST(context.Background(), exec, base, "sim-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "project-ids=p1") || !strings.Contains(gotQuery, "project-ids=p2") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetSASTLatest(t *testing.T) {
	var gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"predicateHistoryPerProject":[],"totalCount":0}`)
	})
	_, err := predicates.GetSASTLatest(context.Background(), exec, base, "sim-1", []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/sim-1/latest") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetSASTWithScan(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"predicateHistoryPerProject":[],"totalCount":0}`)
	})
	_, err := predicates.GetSASTWithScan(context.Background(), exec, base, "sim-1", "scan-1", []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "scan-id=scan-1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestCreateCustomState(t *testing.T) {
	var gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":42,"name":"MyState","type":"custom"}`)
	})
	state, err := predicates.CreateCustomState(context.Background(), exec, base, "MyState")
	if err != nil {
		t.Fatal(err)
	}
	if state.ID != 42 || state.Name != "MyState" {
		t.Errorf("state = %+v", state)
	}
	if !strings.Contains(gotBody, `"name":"MyState"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestDeleteCustomState(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := predicates.DeleteCustomState(context.Background(), exec, base, 42)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/42") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetChangeHistory(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"results":[{"similarityId":"sim-1","projectId":"p1"}],"totalSimilarityIds":"presenting 1 of 1"}`)
	})
	resp, err := predicates.GetChangeHistory(context.Background(), exec, base, "projectID", "p1", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %+v", resp.Results)
	}
	if !strings.Contains(gotQuery, "entity-type=projectID") || !strings.Contains(gotQuery, "entity-id=p1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestUpdateSAST(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})
	state := models.PredicateStateConfirmed
	err := predicates.UpdateSAST(context.Background(), exec, base, []models.PredicateRequest{
		{SimilarityID: "sim-1", ProjectID: "p1", State: &state, Severity: "High"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
}
