package clients_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/clients"
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
		_, _ = io.WriteString(w, `[{"id":"c1","clientId":"my-app"}]`)
	})
	out, err := clients.List(context.Background(), exec, base, models.OIDCClientFilter{Max: 10, ClientID: "my-app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ClientID != "my-app" {
		t.Errorf("got %+v", out)
	}
	if !strings.Contains(gotQuery, "max=10") || !strings.Contains(gotQuery, "clientId=my-app") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGet(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/clients/c1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"c1","clientId":"my-app"}`)
	})
	c, err := clients.Get(context.Background(), exec, base, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "c1" {
		t.Errorf("id = %q", c.ID)
	}
}

func TestCreate(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
	})
	err := clients.Create(context.Background(), exec, base, &models.OIDCClient{ClientID: "new-app"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"clientId":"new-app"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestUpdate(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := clients.Update(context.Background(), exec, base, &models.OIDCClient{ID: "c1", ClientID: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/clients/c1" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestDelete(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := clients.Delete(context.Background(), exec, base, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestGetSecret(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/client-secret") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"type":"secret","value":"s3cr3t"}`)
	})
	resp, err := clients.GetSecret(context.Background(), exec, base, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Value != "s3cr3t" {
		t.Errorf("value = %q", resp.Value)
	}
}

func TestRegenerateSecret(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_, _ = io.WriteString(w, `{"type":"secret","value":"new-secret"}`)
	})
	resp, err := clients.RegenerateSecret(context.Background(), exec, base, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if resp.Value != "new-secret" {
		t.Errorf("value = %q", resp.Value)
	}
}

func TestAddDefaultScope(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := clients.AddDefaultScope(context.Background(), exec, base, "c1", "scope-id")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/clients/c1/default-client-scopes/scope-id" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestGetServiceAccountUser(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/service-account-user") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"u-sa","username":"service-account-my-app"}`)
	})
	u, err := clients.GetServiceAccountUser(context.Background(), exec, base, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "u-sa" {
		t.Errorf("id = %q", u.ID)
	}
}

func TestListScopes(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client-scopes" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"s1","name":"groups","protocol":"openid-connect"}]`)
	})
	scopes, err := clients.ListScopes(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0].Name != "groups" {
		t.Errorf("scopes = %+v", scopes)
	}
}
