package cxone_test

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestScansHandleLatestCompletedAndNewFindings(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"totalCount":2,"filteredTotalCount":2,"scans":[{"id":"q","status":"Queued"},{"id":"ok","status":"Completed","branch":"main"}]}`)
		},
		"/api/results": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("offset") != "" && r.URL.Query().Get("offset") != "0" {
				_, _ = io.WriteString(w, `{"results":[],"totalCount":0}`)
				return
			}
			if r.URL.Query().Get("scan-id") == "new" {
				_, _ = io.WriteString(w, `{"results":[{"id":"1","similarityId":"a","severity":"HIGH","type":"sast"}],"totalCount":1}`)
				return
			}
			_, _ = io.WriteString(w, `{"results":[],"totalCount":0}`)
		},
	})
	s, err := c.Scans().LatestCompleted(context.Background(), "p1", "main")
	if err != nil || s == nil || s.ID != "ok" {
		t.Fatalf("LatestCompleted = %+v, %v", s, err)
	}
	cmp, err := c.Scans().NewFindings(context.Background(), "old", "new", 2000)
	if err != nil || len(cmp.NewFindings) != 1 || cmp.Truncated {
		t.Fatalf("NewFindings = %+v, %v", cmp, err)
	}
}
