package results_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/results"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *results.Backend {
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
	return &results.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestReaderListAllSetsScanIDFromArgument(t *testing.T) {
	var got url.Values
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		// Page 1 → 1 result, subsequent pages → empty to terminate the iterator.
		if r.URL.Query().Get("offset") != "0" {
			_, _ = io.WriteString(w, `{"results":[],"totalCount":1,"scanID":"scan-abc"}`)
			return
		}
		_, _ = io.WriteString(w, `{"results":[{"id":"r1","similarityId":"sim-1","severity":"HIGH"}],"totalCount":1,"scanID":"scan-abc"}`)
	})

	reader := results.NewReader(b)
	rs, err := reader.ListAll(context.Background(), "scan-abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].ID != "r1" {
		t.Errorf("ListAll = %+v", rs)
	}
	if got.Get("scan-id") != "scan-abc" {
		t.Errorf("scan-id query param = %q, want scan-abc", got.Get("scan-id"))
	}
}

func TestReaderListAllRequiresScanID(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should NOT be called when scanID is empty")
	})
	reader := results.NewReader(b)
	if _, err := reader.ListAll(context.Background(), "", nil); err == nil {
		t.Fatal("ListAll with empty scanID should return an error")
	}
}

func TestReaderListByStateAddsStateFilter(t *testing.T) {
	var got url.Values
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = io.WriteString(w, `{"results":[],"totalCount":0,"scanID":"s"}`)
	})

	reader := results.NewReader(b)
	if _, err := reader.ListByState(context.Background(), "s", "NOT_EXPLOITABLE"); err != nil {
		t.Fatal(err)
	}
	if got.Get("scan-id") != "s" {
		t.Errorf("scan-id = %q", got.Get("scan-id"))
	}
	if got.Get("state") != "NOT_EXPLOITABLE" {
		t.Errorf("state = %q", got.Get("state"))
	}
}

func TestReaderIterStreamsAcrossPages(t *testing.T) {
	page := 0
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		page++
		if r.URL.Query().Get("scan-id") != "s" {
			t.Errorf("scan-id missing on page %d", page)
		}
		switch page {
		case 1:
			_, _ = io.WriteString(w, `{"results":[{"id":"r1"},{"id":"r2"}],"totalCount":2,"scanID":"s"}`)
		default:
			_, _ = io.WriteString(w, `{"results":[],"totalCount":0,"scanID":"s"}`)
		}
	})

	reader := results.NewReader(b)
	it := reader.Iter("s", nil)
	var seen []string
	for {
		r, ok, err := it.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		seen = append(seen, r.ID)
	}
	if len(seen) != 2 || seen[0] != "r1" || seen[1] != "r2" {
		t.Errorf("seen = %v", seen)
	}
}

func TestReaderRejectsNilBackend(t *testing.T) {
	reader := results.NewReader(nil)
	if _, err := reader.ListAll(context.Background(), "s", nil); err == nil {
		t.Fatal("ListAll with nil backend should error")
	}
}
