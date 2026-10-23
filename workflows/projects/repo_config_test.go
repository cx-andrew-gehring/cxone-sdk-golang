package projects_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)              { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error)    { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *projects.Backend {
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
	return &projects.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestFromProjectIDFetchesProject(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/p1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"p1","name":"acme","mainBranch":"main","repoUrl":"git@x"}`)
	})
	cfg, err := projects.FromProjectID(context.Background(), c, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID() != "p1" || cfg.Name() != "acme" || cfg.MainBranch() != "main" || cfg.RepoURL() != "git@x" {
		t.Errorf("unexpected: %+v", cfg.Project())
	}
}

func TestFromProjectIDPropagatesNotFound(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"not found","code":404}`)
	})
	_, err := projects.FromProjectID(context.Background(), c, "missing")
	var rerr *cxerrors.ResponseError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected ResponseError, got %T: %v", err, err)
	}
	if rerr.StatusCode != 404 {
		t.Errorf("status = %d", rerr.StatusCode)
	}
}

func TestConfigurationLazyAndCached(t *testing.T) {
	var configHits, projectHits int32
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/p1":
			atomic.AddInt32(&projectHits, 1)
			_, _ = io.WriteString(w, `{"id":"p1","name":"acme"}`)
		case "/api/configuration/project":
			atomic.AddInt32(&configHits, 1)
			_, _ = io.WriteString(w, `[{"key":"sast.preset","value":"ASA","valuetype":"String","name":"preset","category":"sast","originLevel":"project","allowOverride":true}]`)
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	})
	cfg, err := projects.FromProjectID(context.Background(), c, "p1")
	if err != nil {
		t.Fatal(err)
	}
	// First call: triggers a fetch.
	out1, err := cfg.Configuration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out1) != 1 || out1[0].Key != "sast.preset" {
		t.Errorf("got %+v", out1)
	}
	// Second call: cached.
	out2, err := cfg.Configuration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if &out1[0] != &out2[0] {
		// Same backing slice element pointer = same cached slice.
		t.Errorf("Configuration returned a different cached slice on second call")
	}
	if hits := atomic.LoadInt32(&configHits); hits != 1 {
		t.Errorf("config hits = %d, want 1", hits)
	}
	if hits := atomic.LoadInt32(&projectHits); hits != 1 {
		t.Errorf("project hits = %d, want 1", hits)
	}
}

func TestConfigurationConcurrentCallersShareOneFetch(t *testing.T) {
	var hits int32
	// gate blocks the first /api/configuration/project request long enough
	// for the other goroutines to queue up on the sync.Once. We close it
	// once instead of toggling a shared bool, so there's no data race
	// between the test goroutine and the httptest handler goroutines.
	gate := make(chan struct{})
	defer func() {
		// Best-effort close in case the test fails before reaching the
		// explicit close below.
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/projects/p1" {
			_, _ = io.WriteString(w, `{"id":"p1","name":"acme"}`)
			return
		}
		atomic.AddInt32(&hits, 1)
		<-gate // first goroutine blocks here; others should never reach this
		_, _ = io.WriteString(w, `[]`)
	})
	cfg, err := projects.FromProjectID(context.Background(), c, "p1")
	if err != nil {
		t.Fatal(err)
	}

	const N = 16
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := cfg.Configuration(context.Background()); err != nil {
				t.Errorf("config: %v", err)
			}
		}()
	}
	// Give other goroutines a moment to pile up on the Once before we
	// release the in-flight request. The release is unconditional so the
	// in-flight goroutine eventually completes its sync.Once Do call.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected exactly 1 config fetch, got %d", got)
	}
}

func TestConfigurationFailureResetsForRetry(t *testing.T) {
	var attempts int32
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/p1":
			_, _ = io.WriteString(w, `{"id":"p1"}`)
		case "/api/configuration/project":
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = io.WriteString(w, `[]`)
		}
	})
	cfg, err := projects.FromProjectID(context.Background(), c, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Configuration(context.Background()); err == nil {
		t.Fatal("expected error on first attempt")
	}
	// After failure, a fresh call should retry rather than return the
	// stale error.
	out, err := cfg.Configuration(context.Background())
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if len(out) != 0 {
		t.Errorf("got %v", out)
	}
}

func TestFromProjectModelSkipsHTTP(t *testing.T) {
	var hits int32
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError) // would fail if reached
	})
	cfg, err := projects.FromProjectModel(c, &models.ProjectResponseModel{
		ID: "p1", Name: "acme", MainBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID() != "p1" || cfg.Name() != "acme" || cfg.MainBranch() != "main" {
		t.Errorf("unexpected: %+v", cfg.Project())
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("hits = %d, want 0 (no HTTP)", got)
	}
}
