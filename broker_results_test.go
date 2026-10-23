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
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// pagedHandler serves pages[i] for the i-th page when paging by page number
// (offset=i) or by item count (offset=i*limit), and an empty page afterwards.
func pagedHandler(t *testing.T, key string, pages []string, byCount bool, seen *[]url.Values) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		*seen = append(*seen, q)
		off := 0
		if v := q.Get("offset"); v != "" {
			off, _ = strconv.Atoi(v)
		}
		lim := 1
		if v := q.Get("limit"); v != "" {
			lim, _ = strconv.Atoi(v)
		}
		idx := off
		if byCount {
			idx = off / lim
		}
		if idx < len(pages) {
			_, _ = io.WriteString(w, `{"`+key+`":[`+pages[idx]+`],"totalCount":9}`)
			return
		}
		_, _ = io.WriteString(w, `{"`+key+`":[],"totalCount":9}`)
	}
}

func TestAdvancedResultsIterRawPreservesBytesAndPages(t *testing.T) {
	var seen []url.Values
	// Unknown field + odd spacing must survive byte-for-byte.
	a := `{"id":"r1",  "zzUnknown":{"x":[1,2]}}`
	b := `{"id":"r2"}`
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/results": pagedHandler(t, "results", []string{a, b}, false, &seen),
	})
	it := c.Advanced().Results().IterRaw(url.Values{"scan-id": {"s1"}}, pagination.Config{PageSize: 1})
	got, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || string(got[0]) != a || string(got[1]) != b {
		t.Fatalf("got %q", got)
	}
	// Spec default: offset counts pages (0,1,2).
	if seen[1].Get("offset") != "1" || seen[2].Get("offset") != "2" {
		t.Errorf("offsets: %v %v", seen[1].Get("offset"), seen[2].Get("offset"))
	}
	if seen[0].Get("scan-id") != "s1" || seen[0].Get("limit") != "1" {
		t.Errorf("query = %v", seen[0])
	}
}

func TestResultsStreamEmptyScan(t *testing.T) {
	var seen []url.Values
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/results": pagedHandler(t, "results", nil, false, &seen),
	})
	calls := 0
	if err := c.Results().Stream(context.Background(), "s1", func(json.RawMessage) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("fn called %d times", calls)
	}
}

func TestResultsStreamMultiPageAndPagingModeConfigurable(t *testing.T) {
	var seen []url.Values
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/results": pagedHandler(t, "results", []string{`{"id":"1"},{"id":"2"}`, `{"id":"3"}`}, true, &seen),
	})
	var ids []string
	h := c.Results().WithPagination(pagination.Config{PageSize: 2, OffsetMode: pagination.OffsetModeItems})
	err := h.Stream(context.Background(), "s1", func(m json.RawMessage) error {
		var x struct{ ID string }
		_ = json.Unmarshal(m, &x)
		ids = append(ids, x.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "1,2,3" {
		t.Errorf("ids = %v", ids)
	}
	if seen[1].Get("offset") != "2" {
		t.Errorf("item-count offset = %q", seen[1].Get("offset"))
	}
}

func TestResultsStreamCallbackErrorAndCancellation(t *testing.T) {
	var seen []url.Values
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/results": pagedHandler(t, "results", []string{`{"id":"1"}`, `{"id":"2"}`}, false, &seen),
	})
	boom := errors.New("boom")
	if err := c.Results().Stream(context.Background(), "s1", func(json.RawMessage) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("callback error not propagated: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err := c.Results().Stream(ctx, "s1", func(json.RawMessage) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
	if err := c.Results().Stream(context.Background(), "", func(json.RawMessage) error { return nil }); err == nil {
		t.Error("empty scanID should error")
	}
}

func TestResultsStreamSASTVisibleColumns(t *testing.T) {
	var seen []url.Values
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		// sast-results: offset counts results (spec) -> byCount.
		"/api/sast-results": pagedHandler(t, "results", []string{`{"resultId":"a","isAIGenerated":true}`, `{"resultId":"b"}`}, true, &seen),
	})
	var n int
	err := c.Results().WithPagination(pagination.Config{PageSize: 1}).StreamSAST(context.Background(), "s1",
		[]string{"result-id", "is-ai-generated"}, func(json.RawMessage) error { n++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("n = %d", n)
	}
	q := seen[0]
	if q.Get("scan-id") != "s1" || strings.Join(q["visible-columns"], ",") != "result-id,is-ai-generated" {
		t.Errorf("query = %v", q)
	}
	if seen[1].Get("offset") != "1" { // spec default for sast-results: item count, PageSize 1
		t.Errorf("offset = %q", seen[1].Get("offset"))
	}
}
