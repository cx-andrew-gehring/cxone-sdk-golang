package access_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/access"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
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

func TestGetAssignment(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"entityID":"u1","entityType":"user","resourceID":"p1","resourceType":"project"}`)
	})
	resp, err := access.GetAssignment(context.Background(), exec, base, "u1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.EntityID != "u1" || resp.ResourceID != "p1" {
		t.Errorf("got %+v", resp)
	}
	if !strings.Contains(gotQuery, "entity-id=u1") || !strings.Contains(gotQuery, "resource-id=p1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestDeleteAssignment(t *testing.T) {
	var gotMethod, gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})
	err := access.DeleteAssignment(context.Background(), exec, base, "u1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotQuery, "entity-id=u1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestResourcesFor(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"entityID":"u1","resourceID":"p1","resourceType":"project"}]`)
	})
	out, err := access.ResourcesFor(context.Background(), exec, base, "u1", "user", []string{"project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if !strings.Contains(gotQuery, "entity-id=u1") || !strings.Contains(gotQuery, "entity-type=user") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestHasAccess(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"accessGranted":true}`)
	})
	granted, err := access.HasAccess(context.Background(), exec, base, "p1", "project", "manage-scan")
	if err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Error("expected granted=true")
	}
}

func TestAccessibleResources(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"all":false,"resources":[{"resourceId":"p1","resourceType":"project","resourceName":"proj1","roles":["admin"]}]}`)
	})
	resp, err := access.AccessibleResources(context.Background(), exec, base, []string{"project"}, "manage-scan")
	if err != nil {
		t.Fatal(err)
	}
	if resp.All {
		t.Error("expected all=false")
	}
	if len(resp.Resources) != 1 || resp.Resources[0].ResourceName != "proj1" {
		t.Errorf("resources = %+v", resp.Resources)
	}
}
