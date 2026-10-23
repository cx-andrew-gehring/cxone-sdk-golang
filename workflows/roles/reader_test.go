package roles_test

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
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/roles"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *roles.Backend {
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
	return &roles.Backend{Executor: exec, IAMAdminURL: srv.URL + "/"}
}

func TestReaderListAndByName(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"r1","name":"admin"},{"id":"r2","name":"viewer","composite":true}]`)
	})
	reader := roles.NewReader(b)

	list, err := reader.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(list))
	}

	role, err := reader.ByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if role == nil || role.ID != "r1" {
		t.Errorf("ByName(admin) = %v", role)
	}

	role, _ = reader.ByName(context.Background(), "nonexistent")
	if role != nil {
		t.Errorf("expected nil for nonexistent, got %v", role)
	}
}

func TestReaderByID(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"r1","name":"admin"}]`)
	})
	reader := roles.NewReader(b)
	role, err := reader.ByID(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if role == nil || role.Name != "admin" {
		t.Errorf("ByID(r1) = %v", role)
	}
}

func TestReaderCachesOnce(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `[{"id":"r1","name":"admin"}]`)
	})
	reader := roles.NewReader(b)
	_, _ = reader.List(context.Background())
	_, _ = reader.ByName(context.Background(), "admin")
	_, _ = reader.ByID(context.Background(), "r1")

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 fetch, got %d", got)
	}
}

func TestReaderCompositesLazyFetch(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "roles-by-id") {
			_, _ = io.WriteString(w, `[{"id":"sub1","name":"sub-role"}]`)
			return
		}
		_, _ = io.WriteString(w, `[{"id":"r1","name":"composite-role","composite":true}]`)
	})
	reader := roles.NewReader(b)

	subs, err := reader.Composites(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Name != "sub-role" {
		t.Errorf("Composites = %v", subs)
	}
}

func TestReaderCompositesNilForNonComposite(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"r1","name":"simple","composite":false}]`)
	})
	reader := roles.NewReader(b)
	subs, err := reader.Composites(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if subs != nil {
		t.Errorf("expected nil for non-composite, got %v", subs)
	}
}
