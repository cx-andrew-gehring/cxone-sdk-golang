package migration_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/migration"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *migration.Backend {
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
	return &migration.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestFromImportID(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"migrationId":"m1","status":"running"}`)
	})
	insp, err := migration.FromImportID(context.Background(), b, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if insp.ID() != "m1" {
		t.Errorf("id = %q", insp.ID())
	}
	if insp.Status() != "running" {
		t.Errorf("status = %q", insp.Status())
	}
	if insp.Terminal() {
		t.Error("running should not be terminal")
	}
}

func TestWaitUntilCompleteHappyPath(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n <= 2 {
			_, _ = io.WriteString(w, `{"migrationId":"m1","status":"running"}`)
		} else {
			_, _ = io.WriteString(w, `{"migrationId":"m1","status":"completed"}`)
		}
	})
	insp, _ := migration.FromImportID(context.Background(), b, "m1")
	final, err := migration.WaitUntilComplete(context.Background(), insp, migration.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Completed() {
		t.Errorf("expected completed, got %q", final.Status())
	}
}

func TestWaitUntilCompleteFailure(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"migrationId":"m1","status":"failed","logs":[{"level":"error","msg":"bad data"}]}`)
	})
	insp, _ := migration.FromImportID(context.Background(), b, "m1")
	_, err := migration.WaitUntilComplete(context.Background(), insp, migration.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error for failed import")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error = %q", err)
	}
}

func TestWaitUntilCompleteCancellation(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"migrationId":"m1","status":"running"}`)
	})
	insp, _ := migration.FromImportID(context.Background(), b, "m1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := migration.WaitUntilComplete(ctx, insp, migration.WaitOptions{
		PollInterval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestWaitUntilCompletePartial(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"migrationId":"m1","status":"partial"}`)
	})
	insp, _ := migration.FromImportID(context.Background(), b, "m1")
	final, err := migration.WaitUntilComplete(context.Background(), insp, migration.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Completed() {
		t.Errorf("partial should count as completed, got %q", final.Status())
	}
}
