package cxone_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	wfprojects "github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
)

func TestClientProjectsGetOrCreateForRepo(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/projects": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"p9","name":"acme","repoUrl":"https://x/r"}`)
		},
	})
	got, err := c.Projects().GetOrCreateForRepo(context.Background(), "acme", wfprojects.CreateOpts{RepoURL: "https://x/r"})
	if err != nil || got.ID() != "p9" {
		t.Fatalf("got %v err %v", got, err)
	}
}
