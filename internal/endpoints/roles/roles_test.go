package roles_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/roles"
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

func TestListRealm(t *testing.T) {
	var gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `[{"id":"r1","name":"admin"},{"id":"r2","name":"viewer"}]`)
	})
	out, err := roles.ListRealm(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	if gotPath != "/roles" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetRealmByName(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/roles/admin") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"r1","name":"admin"}`)
	})
	r, err := roles.GetRealmByName(context.Background(), exec, base, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "r1" {
		t.Errorf("id = %q", r.ID)
	}
}

func TestSearchRealm(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"r1","name":"admin-full"}]`)
	})
	out, err := roles.SearchRealm(context.Background(), exec, base, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if !strings.Contains(gotQuery, "search=admin") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestListClient(t *testing.T) {
	var gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `[{"id":"r1","name":"ast-admin","clientRole":true}]`)
	})
	out, err := roles.ListClient(context.Background(), exec, base, "cid-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if gotPath != "/clients/cid-123/roles" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetClientByName(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"r1","name":"ast-admin"}`)
	})
	r, err := roles.GetClientByName(context.Background(), exec, base, "cid", "ast-admin")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "ast-admin" {
		t.Errorf("name = %q", r.Name)
	}
}

func TestCreateClient(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	})
	err := roles.CreateClient(context.Background(), exec, base, "cid", &models.Role{Name: "custom-role"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if gotPath != "/clients/cid/roles" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetByID(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "roles-by-id/r1") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"r1","name":"admin"}`)
	})
	r, err := roles.GetByID(context.Background(), exec, base, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "admin" {
		t.Errorf("name = %q", r.Name)
	}
}

func TestDeleteByID(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := roles.DeleteByID(context.Background(), exec, base, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestGetComposites(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/composites") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"s1","name":"sub-role"}]`)
	})
	subs, err := roles.GetComposites(context.Background(), exec, base, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Name != "sub-role" {
		t.Errorf("composites = %+v", subs)
	}
}

func TestAddComposites(t *testing.T) {
	var gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	err := roles.AddComposites(context.Background(), exec, base, "r1", []models.Role{{ID: "s1"}, {ID: "s2"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"id":"s1"`) || !strings.Contains(gotBody, `"id":"s2"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestRemoveComposites(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := roles.RemoveComposites(context.Background(), exec, base, "r1", []models.Role{{ID: "s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}
