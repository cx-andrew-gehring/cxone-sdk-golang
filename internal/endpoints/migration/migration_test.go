package migration_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/migration"
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

func TestStartImport(t *testing.T) {
	var gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"migrationId":"m1"}`)
	})
	id, err := migration.StartImport(context.Background(), exec, base, "data.zip", "mapping.csv", "key123")
	if err != nil {
		t.Fatal(err)
	}
	if id != "m1" {
		t.Errorf("migrationId = %q", id)
	}
	if !strings.Contains(gotBody, `"fileName":"data.zip"`) {
		t.Errorf("body = %q", gotBody)
	}
	if !strings.Contains(gotBody, `"encryptionKey":"key123"`) {
		t.Errorf("body missing key: %q", gotBody)
	}
}

func TestStartImportValidation(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	_, err := migration.StartImport(context.Background(), exec, base, "", "", "key")
	if err == nil {
		t.Error("expected error for empty fileName")
	}
	_, err = migration.StartImport(context.Background(), exec, base, "data.zip", "", "")
	if err == nil {
		t.Error("expected error for empty encryptionKey")
	}
}

func TestList(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/imports" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"migrationId":"m1","status":"completed"},{"migrationId":"m2","status":"running"}]`)
	})
	imports, err := migration.List(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 2 {
		t.Fatalf("expected 2, got %d", len(imports))
	}
}

func TestGet(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/m1") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"migrationId":"m1","status":"completed","logs":[{"level":"info","msg":"done"}]}`)
	})
	imp, err := migration.Get(context.Background(), exec, base, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if imp.MigrationID != "m1" || imp.Status != "completed" {
		t.Errorf("got %+v", imp)
	}
	if len(imp.Logs) != 1 {
		t.Errorf("logs = %+v", imp.Logs)
	}
}
