package applications_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/applications"
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

func TestCreate(t *testing.T) {
	var gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"a1","name":"my-app","criticality":3}`)
	})
	app, err := applications.Create(context.Background(), exec, base, &models.ApplicationCreateRequest{Name: "my-app"})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != "a1" {
		t.Errorf("id = %q", app.ID)
	}
	if !strings.Contains(gotBody, `"rules":[]`) {
		t.Errorf("expected empty rules default in body: %q", gotBody)
	}
	if !strings.Contains(gotBody, `"tags":{}`) {
		t.Errorf("expected empty tags default in body: %q", gotBody)
	}
}

func TestGet(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/a1") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"a1","name":"my-app"}`)
	})
	app, err := applications.Get(context.Background(), exec, base, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if app.Name != "my-app" {
		t.Errorf("name = %q", app.Name)
	}
}

func TestDelete(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := applications.Delete(context.Background(), exec, base, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestPatch(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	name := "updated"
	err := applications.Patch(context.Background(), exec, base, "a1", &models.ApplicationPatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"name":"updated"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestCount(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"totalCount":10,"filteredTotalCount":7,"applications":[]}`)
	})
	n, err := applications.Count(context.Background(), exec, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("count = %d (expected filteredTotalCount)", n)
	}
}
