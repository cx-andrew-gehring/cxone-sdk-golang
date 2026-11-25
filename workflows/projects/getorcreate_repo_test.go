package projects_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
)

func TestGetOrCreateForRepoConcurrentCreatesOnce(t *testing.T) {
	var creates int32
	var mu sync.Mutex
	created := false
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			if created {
				_, _ = io.WriteString(w, `{"totalCount":1,"filteredTotalCount":1,"projects":[{"id":"p1","name":"acme","repoUrl":"https://x/r"}]}`)
			} else {
				_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects":
			atomic.AddInt32(&creates, 1)
			if created {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"message":"project name 'acme' already exists"}`)
				return
			}
			created = true
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"p1","name":"acme","repoUrl":"https://x/r"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := projects.GetOrCreateForRepo(context.Background(), b, "acme", projects.CreateOpts{RepoURL: "https://x/r"})
			if err != nil || got.ID() != "p1" {
				t.Errorf("got %v err %v", got, err)
			}
		}()
	}
	wg.Wait()
	if n := atomic.LoadInt32(&creates); n != 1 {
		t.Fatalf("create POSTs = %d, want 1", n)
	}
}

func TestGetOrCreateForRepoSendsRepoURLTagsOrigin(t *testing.T) {
	var body map[string]any
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		body["id"] = "p1"
		_ = json.NewEncoder(w).Encode(body) // server echoes the created project
	})
	_, err := projects.GetOrCreateForRepo(context.Background(), b, "acme", projects.CreateOpts{
		RepoURL: "https://x/r", Origin: "scm-broker",
		Tags: map[string]string{"team": "a"}, Groups: []string{"g1", "g2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["name"] != "acme" || body["repoUrl"] != "https://x/r" || body["origin"] != "scm-broker" {
		t.Errorf("body = %v", body)
	}
	if tags, _ := body["tags"].(map[string]any); tags["team"] != "a" {
		t.Errorf("tags = %v", body["tags"])
	}
	if g, _ := body["groups"].([]any); len(g) != 2 || g[0] != "g1" {
		t.Errorf("groups = %v", body["groups"])
	}
}

func TestGetOrCreateForRepoUpdatesExistingRepoURLAndTags(t *testing.T) {
	var patch map[string]any
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"totalCount":1,"filteredTotalCount":1,"projects":[{"id":"p1","name":"acme","repoUrl":"https://old","tags":{"keep":"1"}}]}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/projects/p1":
			_ = json.NewDecoder(r.Body).Decode(&patch)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	got, err := projects.GetOrCreateForRepo(context.Background(), b, "acme", projects.CreateOpts{
		RepoURL: "https://new", Tags: map[string]string{"team": "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if patch["repoUrl"] != "https://new" {
		t.Errorf("patch = %v", patch)
	}
	tags, _ := patch["tags"].(map[string]any)
	if tags["keep"] != "1" || tags["team"] != "a" {
		t.Errorf("tags = %v (existing tags must be preserved)", tags)
	}
	if got.RepoURL() != "https://new" || got.Tags()["team"] != "a" {
		t.Errorf("handle not refreshed: %q %v", got.RepoURL(), got.Tags())
	}
}

func TestGetOrCreateForRepoNoPatchWhenUnchanged(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s", r.Method)
		}
		_, _ = io.WriteString(w, `{"totalCount":1,"filteredTotalCount":1,"projects":[{"id":"p1","name":"acme","repoUrl":"https://x","tags":{"team":"a"}}]}`)
	})
	if _, err := projects.GetOrCreateForRepo(context.Background(), b, "acme", projects.CreateOpts{
		RepoURL: "https://x", Tags: map[string]string{"team": "a"},
	}); err != nil {
		t.Fatal(err)
	}
}
