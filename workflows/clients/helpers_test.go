package clients_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/clients"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *clients.Backend {
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
	return &clients.Backend{Executor: exec, IAMAdminURL: srv.URL + "/"}
}

func TestGetOrCreateByNameFindsExisting(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"c1","clientId":"my-client","enabled":true}]`)
	})
	c, err := clients.GetOrCreateByName(context.Background(), b, "my-client", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "c1" {
		t.Errorf("expected c1, got %q", c.ID)
	}
}

func TestGetOrCreateByNameCreatesNew(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		switch {
		case r.Method == http.MethodGet && n == 1:
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `[{"id":"c-new","clientId":"new-client","enabled":true}]`)
		}
	})
	c, err := clients.GetOrCreateByName(context.Background(), b, "new-client", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "c-new" {
		t.Errorf("expected c-new, got %q", c.ID)
	}
}

func TestGetOrCreateByNamePassesDefaults(t *testing.T) {
	var capturedBody string
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			capturedBody = string(body)
			w.WriteHeader(http.StatusCreated)
			return
		}
		_, _ = io.WriteString(w, `[{"id":"c1","clientId":"test","enabled":true}]`)
	})
	_, _ = clients.GetOrCreateByName(context.Background(), b, "test", &models.OIDCClient{
		Enabled: true,
	})
	if !strings.Contains(capturedBody, `"serviceAccountsEnabled":true`) {
		t.Errorf("expected serviceAccountsEnabled in body, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"protocol":"openid-connect"`) {
		t.Errorf("expected protocol default in body, got %s", capturedBody)
	}
}

func TestASTAppIDCacheFetchesOnce(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `[{"id":"ast-app-uuid","clientId":"ast-app"}]`)
	})
	cache := clients.NewASTAppIDCache(b)
	id, err := cache.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != "ast-app-uuid" {
		t.Errorf("expected ast-app-uuid, got %q", id)
	}

	id2, _ := cache.Get(context.Background())
	if id2 != id {
		t.Errorf("second call returned %q", id2)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 fetch, got %d", got)
	}
}
