package advanced_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// TestProjectsIterWalksMultiplePagesEndToEnd verifies the iterator's
// integration with the projects endpoint package: build a real client,
// point it at an httptest server that returns three pages plus an empty
// terminator, and confirm the iterator surfaces all items in order, sets
// the right offset/limit query params on each page, and stops on the
// empty page.
func TestProjectsIterWalksMultiplePagesEndToEnd(t *testing.T) {
	var (
		seenOffsets []int32
		hits        int32
	)
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/projects": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
			off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			seenOffsets = append(seenOffsets, int32(off))
			if lim != 2 {
				t.Errorf("limit = %d, want 2", lim)
			}
			// 5 projects total → pages 0..1, 2..3, 4..4, then empty.
			all := []string{"p1", "p2", "p3", "p4", "p5"}
			start := off
			end := off + lim
			if start >= len(all) {
				_, _ = io.WriteString(w, `{"totalCount":5,"filteredTotalCount":5,"projects":[]}`)
				return
			}
			if end > len(all) {
				end = len(all)
			}
			var items []string
			for i := start; i < end; i++ {
				items = append(items, fmt.Sprintf(`{"id":%q,"name":%q}`, all[i], all[i]))
			}
			body := fmt.Sprintf(`{"totalCount":5,"filteredTotalCount":5,"projects":[%s]}`, strings.Join(items, ","))
			_, _ = io.WriteString(w, body)
		},
	})

	it := c.Advanced().Projects().Iter(url.Values{"name-contains": {"acme"}}, pagination.Config{
		PageSize:       2,
		PageRetryDelay: pagination.NoRetryDelay,
	})
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 5 {
		t.Errorf("collected %d projects, want 5", len(all))
	}
	for i, p := range all {
		want := fmt.Sprintf("p%d", i+1)
		if p.ID != want {
			t.Errorf("idx %d: id=%q, want %q", i, p.ID, want)
		}
	}

	// 3 pages; the short final page ends the stream (no empty terminator).
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("hits = %d, want 3", got)
	}
	wantOffsets := []int32{0, 2, 4}
	if len(seenOffsets) != len(wantOffsets) {
		t.Fatalf("offsets = %v, want %v", seenOffsets, wantOffsets)
	}
	for i := range wantOffsets {
		if seenOffsets[i] != wantOffsets[i] {
			t.Errorf("offset[%d] = %d, want %d", i, seenOffsets[i], wantOffsets[i])
		}
	}
}

// TestProjectsIterRangeOverFunc verifies the range-over-func adapter works
// against a real httptest backend.
func TestProjectsIterRangeOverFunc(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/projects": func(w http.ResponseWriter, r *http.Request) {
			off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if off == 0 {
				_, _ = io.WriteString(w, `{"totalCount":2,"filteredTotalCount":2,"projects":[{"id":"a","name":"a"},{"id":"b","name":"b"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"totalCount":2,"filteredTotalCount":2,"projects":[]}`)
		},
	})

	it := c.Advanced().Projects().Iter(nil, pagination.Config{
		PageSize:       10,
		PageRetryDelay: pagination.NoRetryDelay,
	})
	var names []string
	for p, err := range it.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, p.Name)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("names = %v", names)
	}
}

// TestProjectsIterPropagatesErrorAfterPageRetries verifies that a server
// returning 500 → CommunicationError on every attempt eventually surfaces
// to the iterator caller (after the per-page retry budget is exhausted).
func TestProjectsIterPropagatesErrorAfterPageRetries(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/projects": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"boom","code":500}`)
		},
	})
	it := c.Advanced().Projects().Iter(nil, pagination.Config{
		PageSize:       10,
		PageRetriesMax: 1,
		PageRetryDelay: pagination.NoRetryDelay,
	})
	_, ok, err := it.Next(context.Background())
	if ok {
		t.Errorf("ok = true, want false")
	}
	if err == nil {
		t.Errorf("err = nil, want non-nil")
	}
}

// TestResultsIterPreservesScanIDAcrossPages exercises the results iterator
// (which keys off scan-id rather than a free-form filter) to confirm the
// caller's scan-id query param survives every page.
func TestResultsIterPreservesScanIDAcrossPages(t *testing.T) {
	var seenScanIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/realms/acme/protocol/openid-connect/token" {
			_, _ = io.WriteString(w, `{"access_token":"tok"}`)
			return
		}
		if r.URL.Path != "/api/results" {
			t.Errorf("path = %q", r.URL.Path)
			return
		}
		seenScanIDs = append(seenScanIDs, r.URL.Query().Get("scan-id"))
		off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if off == 0 {
			_, _ = io.WriteString(w, `{"results":[{"id":"r1"},{"id":"r2"}],"totalCount":4,"scanID":"s1"}`)
			return
		}
		_, _ = io.WriteString(w, `{"results":[{"id":"r3"},{"id":"r4"}],"totalCount":4,"scanID":"s1"}`)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	region, _ := cxone.NewCustomRegion(host, host)
	region, _ = region.WithScheme("http")
	c, err := cxone.NewClient().
		Region(region).Tenant("acme").AgentName("ua").APIKey("k").
		HTTPClient(srv.Client()).
		Retries(1).RetryDelaySeconds(0).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	it := c.Advanced().Results().Iter(url.Values{"scan-id": {"s1"}}, pagination.Config{
		PageSize:       2,
		PageRetryDelay: pagination.NoRetryDelay,
	})
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Errorf("got %d results, want 4", len(all))
	}
	if len(seenScanIDs) != 2 {
		t.Fatalf("seenScanIDs = %v", seenScanIDs)
	}
	for i, id := range seenScanIDs {
		if id != "s1" {
			t.Errorf("page %d: scan-id = %q", i, id)
		}
	}
}
