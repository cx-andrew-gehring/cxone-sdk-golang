package users_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/users"
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
	var gotPath, gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"u1","username":"alice"},{"id":"u2","username":"bob"}]`)
	})
	out, err := users.List(context.Background(), exec, base, models.UserFilter{Max: 50, Email: "test@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 users, got %d", len(out))
	}
	if gotPath != "/users" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "max=50") || !strings.Contains(gotQuery, "email=test") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestCount(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/count") {
			t.Errorf("expected /count path, got %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `42`)
	})
	n, err := users.Count(context.Background(), exec, base, models.UserFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Errorf("count = %d", n)
	}
}

func TestGet(t *testing.T) {
	var gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"u1","username":"alice","email":"alice@x.com"}`)
	})
	u, err := users.Get(context.Background(), exec, base, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "u1" || u.Username != "alice" {
		t.Errorf("got %+v", u)
	}
	if gotPath != "/users/u1" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetValidatesID(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	_, err := users.Get(context.Background(), exec, base, "")
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestCreateParsesLocationHeader(t *testing.T) {
	var callCount int
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "/users/u-new-id")
			w.WriteHeader(http.StatusCreated)
			return
		}
		// Follow-up GET
		_, _ = io.WriteString(w, `{"id":"u-new-id","username":"newuser"}`)
	})
	u, err := users.Create(context.Background(), exec, base, &models.User{Username: "newuser", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "u-new-id" {
		t.Errorf("id = %q", u.ID)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (POST + GET), got %d", callCount)
	}
}

func TestCreateSAMLTwoStepDance(t *testing.T) {
	var calls []string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/users":
			w.Header().Set("Location", "/users/saml-id")
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "saml-id"):
			_, _ = io.WriteString(w, `{"id":"saml-id","username":"samluser","requiredActions":["VERIFY_EMAIL"]}`)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	u, err := users.CreateSAML(context.Background(), exec, base, &models.User{Username: "samluser"}, "okta", "ext-123", "ext-user")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "saml-id" {
		t.Errorf("id = %q", u.ID)
	}
	// POST create, GET fetch, PUT update requiredActions, GET final
	if len(calls) < 4 {
		t.Errorf("expected at least 4 calls, got %d: %v", len(calls), calls)
	}
}

func TestUpdate(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.Update(context.Background(), exec, base, &models.User{ID: "u1", Username: "alice-updated"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q", gotMethod)
	}
	if gotPath != "/users/u1" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDelete(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.Delete(context.Background(), exec, base, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/users/u1" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestGetGroups(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/groups") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"g1","name":"admins"}]`)
	})
	groups, err := users.GetGroups(context.Background(), exec, base, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != "g1" {
		t.Errorf("groups = %+v", groups)
	}
}

func TestAssignGroup(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.AssignGroup(context.Background(), exec, base, "u1", "g1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/users/u1/groups/g1" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestRemoveGroup(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.RemoveGroup(context.Background(), exec, base, "u1", "g1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/users/u1/groups/g1" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestGetClientRoles(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/role-mappings/clients/") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"r1","name":"admin"}]`)
	})
	roles, err := users.GetClientRoles(context.Background(), exec, base, "u1", "ast-app-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "admin" {
		t.Errorf("roles = %+v", roles)
	}
}

func TestAddClientRoles(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.AddClientRoles(context.Background(), exec, base, "u1", "cid", []models.Role{{ID: "r1", Name: "admin"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"id":"r1"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestRemoveClientRoles(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.RemoveClientRoles(context.Background(), exec, base, "u1", "cid", []models.Role{{ID: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestGetRealmRoles(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/role-mappings/realm") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"r1","name":"realm-admin"}]`)
	})
	roles, err := users.GetRealmRoles(context.Background(), exec, base, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "realm-admin" {
		t.Errorf("roles = %+v", roles)
	}
}

func TestAddRealmRoles(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.AddRealmRoles(context.Background(), exec, base, "u1", []models.Role{{ID: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestRemoveRealmRoles(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := users.RemoveRealmRoles(context.Background(), exec, base, "u1", []models.Role{{ID: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}
