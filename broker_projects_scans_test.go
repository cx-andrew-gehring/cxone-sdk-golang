package cxone_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
)

func TestScansListForProjectFiltersSortsAndPages(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			queries = append(queries, r.URL.RawQuery)
			mu.Unlock()
			q := r.URL.Query()
			if q.Get("project-id") != "p1" || q.Get("sort") != "-created_at" {
				t.Errorf("query = %v", q)
			}
			switch q.Get("offset") {
			case "0":
				_, _ = io.WriteString(w, `{"totalCount":2,"scans":[{"id":"s2","status":"Completed","projectId":"p1"},{"id":"s1","status":"Running","projectId":"p1"}]}`)
			default:
				_, _ = io.WriteString(w, `{"totalCount":2,"scans":[]}`)
			}
		},
	})
	got, err := c.Scans().ListForProject(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID() != "s2" || got[1].ID() != "s1" {
		t.Fatalf("got %d inspectors", len(got))
	}
	if _, err := c.Scans().ListForProject(context.Background(), ""); err == nil {
		t.Error("empty projectID should error")
	}
}

func TestScansListForProjectEmptyIsEmptySlice(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"totalCount":0,"scans":[]}`)
		},
	})
	got, err := c.Scans().ListForProject(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want non-nil empty slice, got %#v", got)
	}
}

func TestProjectsDeleteCancelsRunningThenDeletes(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var cancelBodies []string
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			calls = append(calls, "LIST")
			mu.Unlock()
			if r.URL.Query().Get("offset") != "0" {
				_, _ = io.WriteString(w, `{"scans":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"scans":[{"id":"sq","status":"Queued"},{"id":"sr","status":"Running"},{"id":"sc","status":"Completed"}]}`)
		},
		"/api/scans/": func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			calls = append(calls, r.Method+" "+r.URL.Path)
			cancelBodies = append(cancelBodies, string(b))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		},
		"/api/projects/p1": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			calls = append(calls, r.Method+" "+r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		},
	})
	if err := c.Projects().Delete(context.Background(), "p1", true); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(calls, "|")
	if !strings.HasPrefix(got, "LIST|") || !strings.HasSuffix(got, "DELETE /api/projects/p1") ||
		!strings.Contains(got, "PATCH /api/scans/sq") || !strings.Contains(got, "PATCH /api/scans/sr") ||
		strings.Contains(got, "/api/scans/sc") {
		t.Errorf("calls = %s", got)
	}
	for _, b := range cancelBodies {
		if strings.TrimSpace(b) != `{"status":"Canceled"}` {
			t.Errorf("cancel body = %q", b)
		}
	}
}

func TestProjectsDeleteWithoutCancelDoesNotListScans(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans": func(w http.ResponseWriter, r *http.Request) { t.Error("scans listed") },
		"/api/projects/p1": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	})
	if err := c.Projects().Delete(context.Background(), "p1", false); err != nil {
		t.Fatal(err)
	}
}

func TestProjectsDeleteAlreadyGoneIsNil(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans": func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"scans":[]}`) },
		"/api/projects/p1": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"not found"}`)
		},
	})
	if err := c.Projects().Delete(context.Background(), "p1", true); err != nil {
		t.Fatalf("404 should be success, got %v", err)
	}
}

func TestProjectsDeleteScanVanishingDuringCancelIsOK(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/scans": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("offset") != "0" {
				_, _ = io.WriteString(w, `{"scans":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"scans":[{"id":"sr","status":"Running"}]}`)
		},
		"/api/scans/sr": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"/api/projects/p1": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	})
	if err := c.Projects().Delete(context.Background(), "p1", true); err != nil {
		t.Fatal(err)
	}
}

func TestProjectsIterByNameRegexBuildsQuery(t *testing.T) {
	var gotQ []string
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/projects": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			gotQ = append(gotQ, q.Get("name-regex"))
			if q.Get("name") != "" || q.Get("names") != "" {
				t.Errorf("name/names must not be sent with name-regex: %v", q)
			}
			if q.Get("offset") == "0" {
				_, _ = io.WriteString(w, `{"projects":[{"id":"a","name":"fss-1"},{"id":"b","name":"fss-2"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"projects":[]}`)
		},
	})
	all, err := pagination.Collect(context.Background(), c.Projects().IterByNameRegex("^fss-"))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || gotQ[0] != "^fss-" {
		t.Errorf("all=%v q=%v", all, gotQ)
	}
}

func TestProjectsGetOrCreateByNameUsesExactNamesFilter(t *testing.T) {
	created := false
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/projects": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				created = true
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"id":"new","name":"fss-1"}`)
				return
			}
			q := r.URL.Query()
			if q.Get("names") != "fss-1" || q.Get("name") != "" {
				t.Errorf("lookup must use names=<exact>, got %v", q)
			}
			// Simulate the server honouring the exact filter: only the exact project.
			_, _ = io.WriteString(w, `{"projects":[{"id":"exact","name":"fss-1"}]}`)
		},
	})
	repo, err := c.Projects().GetOrCreateByName(context.Background(), &models.Project{Name: "fss-1"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.ID() != "exact" || created {
		t.Errorf("id=%q created=%v", repo.ID(), created)
	}
}
