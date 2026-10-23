package scans_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

func TestInvokerUploadSameHostStorageSendsBearer(t *testing.T) {
	var putAuth string
	var apiURL string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/uploads":
			_, _ = io.WriteString(w, `{"url":"`+apiURL+`/storage/uploads.x/t/o"}`)
		case "/api/scans":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"s1","status":"Queued"}`)
		case "/storage/uploads.x/t/o":
			putAuth = r.Header.Get("Authorization")
		}
	}))
	defer apiSrv.Close()
	apiURL = apiSrv.URL

	exec := transport.NewExecutor(transport.Config{
		HTTPClient: apiSrv.Client(), Authenticator: stubAuth{}, UserAgent: "test",
		RetryPolicy: retry.Policy{MaxAttempts: 1},
	})
	sb := &scans.Backend{Executor: exec, BaseURL: apiSrv.URL + "/"}
	repo, _ := projects.FromProjectModel(&projects.Backend{Executor: exec, BaseURL: apiSrv.URL + "/"},
		&models.ProjectResponseModel{ID: "p1", RepoURL: "g", MainBranch: "main"})
	body := []byte("zip")
	if _, err := scans.NewInvoker(sb, repo, apiSrv.Client()).WithEngines("sast").
		WithUploadReader(bytes.NewReader(body), int64(len(body))).Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if putAuth != "Bearer tok" {
		t.Errorf("PUT Authorization = %q, want Bearer tok", putAuth)
	}
}
