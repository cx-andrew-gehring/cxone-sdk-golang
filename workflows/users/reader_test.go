package users_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/users"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *users.Backend {
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
	return &users.Backend{Executor: exec, IAMAdminURL: srv.URL + "/"}
}

func TestReaderList(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"u1","username":"alice","email":"alice@example.com"},{"id":"u2","username":"bob","email":"bob@example.com"}]`)
	})
	reader := users.NewReader(b)
	list, err := reader.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 users, got %d", len(list))
	}
	if list[0].Username != "alice" || list[1].Username != "bob" {
		t.Errorf("usernames = %q, %q", list[0].Username, list[1].Username)
	}
}

func TestReaderByIDEmailUsername(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"u1","username":"alice","email":"alice@example.com"}]`)
	})
	reader := users.NewReader(b)

	u, err := reader.ByID(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.ID != "u1" {
		t.Errorf("ByID(u1) = %v", u)
	}

	u, err = reader.ByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.Email != "alice@example.com" {
		t.Errorf("ByEmail = %v", u)
	}

	u, err = reader.ByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.Username != "alice" {
		t.Errorf("ByUsername = %v", u)
	}

	u, _ = reader.ByID(context.Background(), "nonexistent")
	if u != nil {
		t.Errorf("expected nil for nonexistent ID, got %v", u)
	}
}

func TestReaderCachesAfterFirstFetch(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `[{"id":"u1","username":"alice"}]`)
	})
	reader := users.NewReader(b)

	_, _ = reader.List(context.Background())
	_, _ = reader.List(context.Background())
	_, _ = reader.ByID(context.Background(), "u1")

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 fetch, got %d", got)
	}
}

func TestReaderPaginates(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		switch n {
		case 1:
			// Return exactly 100 items (page size) to trigger next page fetch.
			items := "["
			for i := 0; i < 100; i++ {
				if i > 0 {
					items += ","
				}
				items += fmt.Sprintf(`{"id":"u%d","username":"user%d"}`, i, i)
			}
			items += "]"
			_, _ = io.WriteString(w, items)
		default:
			_, _ = io.WriteString(w, `[{"id":"u100","username":"user100"}]`)
		}
	})
	reader := users.NewReader(b)
	list, err := reader.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 101 {
		t.Errorf("expected 101 users, got %d", len(list))
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 page fetches, got %d", got)
	}
}

func TestReaderCountDoesNotCache(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `42`)
	})
	reader := users.NewReader(b)
	n, err := reader.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Errorf("count = %d", n)
	}
	n2, _ := reader.Count(context.Background())
	if n2 != 42 {
		t.Errorf("second count = %d", n2)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 direct calls, got %d", got)
	}
}
