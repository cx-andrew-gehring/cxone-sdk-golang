package cxone_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

type reqLog struct {
	mu   sync.Mutex
	seen []url.Values
}

func (l *reqLog) add(q url.Values) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, q)
}
func (l *reqLog) n() int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.seen) }
func (l *reqLog) offsets() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var o []string
	for _, q := range l.seen {
		o = append(o, q.Get("offset"))
	}
	return strings.Join(o, ",")
}

// countingServer serves total items "i0".."iN" honouring offset as pages
// (byCount=false) or items (byCount=true), echoing totalCount.
func countingServer(log *reqLog, total int, byCount bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		log.add(q)
		off, _ := strconv.Atoi(q.Get("offset"))
		lim, _ := strconv.Atoi(q.Get("limit"))
		start := off
		if !byCount {
			start = off * lim
		}
		var items []string
		for i := start; i < start+lim && i < total; i++ {
			items = append(items, `{"id":"i`+strconv.Itoa(i)+`"}`)
		}
		_, _ = io.WriteString(w, `{"results":[`+strings.Join(items, ",")+`],"totalCount":`+strconv.Itoa(total)+`}`)
	}
}

func drain(t *testing.T, fn func(func(json.RawMessage) error) error) (int, error) {
	t.Helper()
	n := 0
	err := fn(func(json.RawMessage) error { n++; return nil })
	return n, err
}

func TestStreamTerminatesWhenServerAlwaysReturnsSamePage(t *testing.T) {
	for _, path := range []string{"/api/results", "/api/sast-results"} {
		log := &reqLog{}
		c := newWorkflowClient(t, map[string]http.HandlerFunc{
			path: func(w http.ResponseWriter, r *http.Request) {
				log.add(r.URL.Query())
				if log.n() > 50 {
					t.Error("runaway pagination")
				}
				_, _ = io.WriteString(w, `{"results":[{"id":"1"}],"totalCount":1}`)
			},
		})
		n, err := drain(t, func(fn func(json.RawMessage) error) error {
			if path == "/api/results" {
				return c.Results().Stream(context.Background(), "s1", fn)
			}
			return c.Results().StreamSAST(context.Background(), "s1", nil, fn)
		})
		if err != nil || n != 1 || log.n() != 1 {
			t.Errorf("%s: n=%d err=%v requests=%d", path, n, err, log.n())
		}
	}
}

func TestStreamTotalCountExactMultipleNoExtraRequest(t *testing.T) {
	for _, tc := range []struct {
		path    string
		mode    pagination.OffsetMode
		byCount bool
		offsets string
	}{
		{"/api/results", pagination.OffsetModePages, false, "0,1"},
		{"/api/results", pagination.OffsetModeItems, true, "0,2"},
		{"/api/sast-results", pagination.OffsetModePages, false, "0,1"},
		{"/api/sast-results", pagination.OffsetModeItems, true, "0,2"},
	} {
		log := &reqLog{}
		c := newWorkflowClient(t, map[string]http.HandlerFunc{tc.path: countingServer(log, 4, tc.byCount)})
		h := c.Results().WithPagination(pagination.Config{PageSize: 2, OffsetMode: tc.mode})
		n, err := drain(t, func(fn func(json.RawMessage) error) error {
			if tc.path == "/api/results" {
				return h.Stream(context.Background(), "s1", fn)
			}
			return h.StreamSAST(context.Background(), "s1", nil, fn)
		})
		if err != nil || n != 4 || log.offsets() != tc.offsets {
			t.Errorf("%s mode=%v: n=%d err=%v offsets=%s want %s", tc.path, tc.mode, n, err, log.offsets(), tc.offsets)
		}
	}
}

func TestStreamShortFinalPage(t *testing.T) {
	log := &reqLog{}
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/sast-results": func(w http.ResponseWriter, r *http.Request) {
			// no totalCount at all: only the short page can end the stream
			log.add(r.URL.Query())
			off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if off == 0 {
				_, _ = io.WriteString(w, `{"results":[{"id":"a"},{"id":"b"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"results":[{"id":"c"}]}`)
		},
	})
	n, err := drain(t, func(fn func(json.RawMessage) error) error {
		return c.Results().WithPagination(pagination.Config{PageSize: 2}).StreamSAST(context.Background(), "s1", nil, fn)
	})
	if err != nil || n != 3 || log.n() != 2 {
		t.Errorf("n=%d err=%v requests=%d", n, err, log.n())
	}
}

func TestStreamOffsetIgnoringServerStalls(t *testing.T) {
	for _, path := range []string{"/api/results", "/api/sast-results"} {
		log := &reqLog{}
		c := newWorkflowClient(t, map[string]http.HandlerFunc{
			path: func(w http.ResponseWriter, r *http.Request) {
				log.add(r.URL.Query())
				if log.n() > 50 {
					t.Error("runaway pagination")
				}
				_, _ = io.WriteString(w, `{"results":[{"id":"1"},{"id":"2"}]}`) // full page, no total, offset ignored
			},
		})
		h := c.Results().WithPagination(pagination.Config{PageSize: 2})
		_, err := drain(t, func(fn func(json.RawMessage) error) error {
			if path == "/api/results" {
				return h.Stream(context.Background(), "s1", fn)
			}
			return h.StreamSAST(context.Background(), "s1", nil, fn)
		})
		if !errors.Is(err, pagination.ErrPaginationStalled) || log.n() > 3 {
			t.Errorf("%s: err=%v requests=%d", path, err, log.n())
		}
	}
}

func TestTypedResultsIterStopsOnTotalCount(t *testing.T) {
	log := &reqLog{}
	c := newWorkflowClient(t, map[string]http.HandlerFunc{"/api/results": countingServer(log, 4, true)})
	it := c.Advanced().Results().Iter(url.Values{"scan-id": {"s1"}}, pagination.Config{PageSize: 2})
	got, err := pagination.Collect(context.Background(), it)
	if err != nil || len(got) != 4 || log.n() != 2 {
		t.Errorf("got=%d err=%v requests=%d", len(got), err, log.n())
	}
}

// Default-mode wire offsets (specs AllScannersResults.yaml:94 -> pages,
// sastResults.yaml:282 -> items). Flipping either default must fail here.
func TestDefaultOffsetModeWireValues(t *testing.T) {
	logR, logS := &reqLog{}, &reqLog{}
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/results":      countingServer(logR, 6, false),
		"/api/sast-results": countingServer(logS, 6, true),
	})
	h := c.Results().WithPagination(pagination.Config{PageSize: 2})
	noop := func(json.RawMessage) error { return nil }
	if err := h.Stream(context.Background(), "s1", noop); err != nil {
		t.Fatal(err)
	}
	if err := h.StreamSAST(context.Background(), "s1", nil, noop); err != nil {
		t.Fatal(err)
	}
	if got := logR.offsets(); got != "0,1,2" {
		t.Errorf("/api/results default offsets = %s, want pages 0,1,2", got)
	}
	if got := logS.offsets(); got != "0,2,4" {
		t.Errorf("/api/sast-results default offsets = %s, want items 0,2,4", got)
	}
	// Same through the Advanced raw iterators.
	logR.seen, logS.seen = nil, nil
	q := url.Values{"scan-id": {"s1"}}
	cfg := pagination.Config{PageSize: 2}
	if _, err := pagination.Collect(context.Background(), c.Advanced().Results().IterRaw(q, cfg)); err != nil {
		t.Fatal(err)
	}
	if _, err := pagination.Collect(context.Background(), c.Advanced().Results().IterSASTRaw(q, cfg)); err != nil {
		t.Fatal(err)
	}
	if logR.offsets() != "0,1,2" || logS.offsets() != "0,2,4" {
		t.Errorf("advanced default offsets: results=%s sast=%s", logR.offsets(), logS.offsets())
	}
}
