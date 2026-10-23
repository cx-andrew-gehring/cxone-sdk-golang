package cxone_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// cappedServer ignores limit above maxLimit (a service-side cap) and always
// reports the real totalCount.
func cappedServer(total, maxLimit int, byCount bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		off, _ := strconv.Atoi(q.Get("offset"))
		lim, _ := strconv.Atoi(q.Get("limit"))
		if lim > maxLimit {
			lim = maxLimit
		}
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

func TestStreamStatsExposeTotalOnTruncation(t *testing.T) {
	for _, path := range []string{"/api/results", "/api/sast-results"} {
		c := newWorkflowClient(t, map[string]http.HandlerFunc{path: cappedServer(10, 3, path == "/api/sast-results")})
		h := c.Results().WithPagination(pagination.Config{PageSize: 5})
		n := 0
		fn := func(json.RawMessage) error { n++; return nil }
		var (
			st  pagination.StreamStats
			err error
		)
		if path == "/api/results" {
			st, err = h.StreamWithStats(context.Background(), "s1", fn)
		} else {
			st, err = h.StreamSASTWithStats(context.Background(), "s1", nil, fn)
		}
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if st.Delivered != 3 || n != 3 || !st.TotalKnown || st.Total != 10 {
			t.Errorf("%s: stats=%+v n=%d, want delivered 3 total 10", path, st, n)
		}
		if !st.Truncated() {
			t.Errorf("%s: Truncated() = false", path)
		}
	}
}

func TestStreamStatsCompleteStream(t *testing.T) {
	log := &reqLog{}
	c := newWorkflowClient(t, map[string]http.HandlerFunc{"/api/results": countingServer(log, 4, false)})
	st, err := c.Results().WithPagination(pagination.Config{PageSize: 2}).StreamWithStats(context.Background(), "s1",
		func(json.RawMessage) error { return nil })
	if err != nil || st.Delivered != 4 || st.Total != 4 || !st.TotalKnown || st.Truncated() {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
}
