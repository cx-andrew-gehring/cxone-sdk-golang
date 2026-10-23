package groups_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/groups"
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

func TestListAdmin(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/groups" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"g1","name":"admins","path":"/admins"}]`)
	})
	out, err := groups.ListAdmin(context.Background(), exec, base, models.GroupFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "admins" {
		t.Errorf("got %+v", out)
	}
}

func TestCount(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/count") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"count":5}`)
	})
	n, err := groups.Count(context.Background(), exec, base, models.GroupFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("count = %d", n)
	}
}

func TestGetByID(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"g1","name":"admins","subGroupCount":2,"realmRoles":["offline_access"]}`)
	})
	g, err := groups.GetByID(context.Background(), exec, base, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if g.SubGroupCount != 2 {
		t.Errorf("subGroupCount = %d", g.SubGroupCount)
	}
	if len(g.RealmRoles) != 1 {
		t.Errorf("realmRoles = %v", g.RealmRoles)
	}
}

func TestGetByPath(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "group-by-path/admins") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"g1","name":"admins","path":"/admins"}`)
	})
	g, err := groups.GetByPath(context.Background(), exec, base, "/admins")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != "g1" {
		t.Errorf("id = %q", g.ID)
	}
}

func TestGetChildren(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"c1","name":"child1"}]`)
	})
	out, err := groups.GetChildren(context.Background(), exec, base, "g1", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if !strings.Contains(gotQuery, "max=50") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestCreateAdmin(t *testing.T) {
	var calls int
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "/groups/g-new")
			w.WriteHeader(http.StatusCreated)
			return
		}
		_, _ = io.WriteString(w, `{"id":"g-new","name":"new-group"}`)
	})
	g, err := groups.CreateAdmin(context.Background(), exec, base, &models.Group{Name: "new-group"})
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != "g-new" {
		t.Errorf("id = %q", g.ID)
	}
	if calls < 2 {
		t.Errorf("expected POST + GET, got %d calls", calls)
	}
}

func TestDeleteAdmin(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := groups.DeleteAdmin(context.Background(), exec, base, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestGetMembers(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"u1","username":"alice"}]`)
	})
	members, err := groups.GetMembers(context.Background(), exec, base, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].Username != "alice" {
		t.Errorf("members = %+v", members)
	}
}

func TestAddRealmRoles(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := groups.AddRealmRoles(context.Background(), exec, base, "g1", []models.Role{{ID: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotPath, "/role-mappings/realm") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestAddClientRoles(t *testing.T) {
	var gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := groups.AddClientRoles(context.Background(), exec, base, "g1", "cid", []models.Role{{ID: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "/role-mappings/clients/cid") {
		t.Errorf("path = %q", gotPath)
	}
}
