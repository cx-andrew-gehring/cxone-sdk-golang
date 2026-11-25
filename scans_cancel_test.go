package cxone_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestScansCancelPatchesCanceled(t *testing.T) {
	var method, path, body string
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans/s1": func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			method, path, body = r.Method, r.URL.Path, string(b)
			w.WriteHeader(http.StatusNoContent)
		},
	})
	if err := c.Scans().Cancel(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || path != "/api/scans/s1" || strings.TrimSpace(body) != `{"status":"Canceled"}` {
		t.Errorf("got %s %s %q", method, path, body)
	}
}

func TestScansCancelIsIdempotent(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusConflict} {
		c := newWorkflowClient(t, map[string]http.HandlerFunc{
			"/api/scans/s1": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = io.WriteString(w, `{"message":"gone or finished"}`)
			},
		})
		if err := c.Scans().Cancel(context.Background(), "s1"); err != nil {
			t.Errorf("status %d should be success, got %v", code, err)
		}
	}
}

func TestScansCancelOtherErrorsSurface(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans/s1": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"no"}`)
		},
	})
	if err := c.Scans().Cancel(context.Background(), "s1"); err == nil {
		t.Error("403 must be an error")
	}
	if err := c.Scans().Cancel(context.Background(), ""); err == nil {
		t.Error("empty id must be an error")
	}
}
