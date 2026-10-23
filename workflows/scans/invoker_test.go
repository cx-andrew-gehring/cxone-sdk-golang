package scans_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

func newScanBackends(t *testing.T, h http.HandlerFunc) (*scans.Backend, *projects.Backend, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    srv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test",
		CorrelationID: "corr",
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	return &scans.Backend{Executor: exec, BaseURL: srv.URL + "/"},
		&projects.Backend{Executor: exec, BaseURL: srv.URL + "/"},
		srv.Client()
}

func TestInvokerStartGitMode(t *testing.T) {
	var gotScanReq models.Scan
	scanBackend, _, client := newScanBackends(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/scans":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotScanReq); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"new-s","status":"Queued","engines":["sast"]}`)
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
	})
	repo, _ := projects.FromProjectModel(&projects.Backend{Executor: scanBackend.Executor, BaseURL: scanBackend.BaseURL}, &models.ProjectResponseModel{
		ID: "p1", Name: "acme", RepoURL: "https://example.com/acme.git", MainBranch: "main",
	})
	insp, err := scans.NewInvoker(scanBackend, repo, client).
		ForBranch("feature").
		WithEngines("sast").
		WithTags(map[string]string{"env": "ci"}).
		Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if insp.ID() != "new-s" {
		t.Errorf("got id %q", insp.ID())
	}
	if gotScanReq.Type != "git" {
		t.Errorf("type = %q", gotScanReq.Type)
	}
	if gotScanReq.Project.ID != "p1" {
		t.Errorf("project id = %q", gotScanReq.Project.ID)
	}
	if gotScanReq.Tags["env"] != "ci" {
		t.Errorf("tags = %v", gotScanReq.Tags)
	}
	// Decode the handler payload and verify branch.
	var h models.GitProjectHandler
	if err := json.Unmarshal(gotScanReq.Handler, &h); err != nil {
		t.Fatal(err)
	}
	if h.Branch != "feature" || h.RepoURL != "https://example.com/acme.git" {
		t.Errorf("handler = %+v", h)
	}
}

func TestInvokerStartFallsBackToMainBranch(t *testing.T) {
	var gotScanReq models.Scan
	scanBackend, _, client := newScanBackends(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotScanReq)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"s","status":"Queued"}`)
	})
	repo, _ := projects.FromProjectModel(&projects.Backend{Executor: scanBackend.Executor, BaseURL: scanBackend.BaseURL}, &models.ProjectResponseModel{
		ID: "p1", RepoURL: "g@x", MainBranch: "develop",
	})
	_, err := scans.NewInvoker(scanBackend, repo, client).
		WithEngines("sast").
		Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var h models.GitProjectHandler
	_ = json.Unmarshal(gotScanReq.Handler, &h)
	if h.Branch != "develop" {
		t.Errorf("branch = %q, expected fallback to develop", h.Branch)
	}
}

func TestInvokerRequiresEngines(t *testing.T) {
	scanBackend, _, client := newScanBackends(t, func(w http.ResponseWriter, r *http.Request) {})
	repo, _ := projects.FromProjectModel(&projects.Backend{Executor: scanBackend.Executor, BaseURL: scanBackend.BaseURL},
		&models.ProjectResponseModel{ID: "p1", RepoURL: "g", MainBranch: "main"})
	_, err := scans.NewInvoker(scanBackend, repo, client).Start(context.Background())
	var ce *cxerrors.ConfigurationError
	if !errors.As(err, &ce) || ce.Field != "Engines" {
		t.Errorf("got %v", err)
	}
}

func TestInvokerRejectsUnknownEngine(t *testing.T) {
	scanBackend, _, client := newScanBackends(t, func(w http.ResponseWriter, r *http.Request) {})
	repo, _ := projects.FromProjectModel(&projects.Backend{Executor: scanBackend.Executor, BaseURL: scanBackend.BaseURL},
		&models.ProjectResponseModel{ID: "p1", RepoURL: "g", MainBranch: "main"})
	_, err := scans.NewInvoker(scanBackend, repo, client).
		WithEngines("rabid-llama").
		Start(context.Background())
	var ce *cxerrors.ConfigurationError
	if !errors.As(err, &ce) {
		t.Errorf("got %v", err)
	}
}

func TestInvokerRejectsMixedSourceModes(t *testing.T) {
	scanBackend, _, client := newScanBackends(t, func(w http.ResponseWriter, r *http.Request) {})
	repo, _ := projects.FromProjectModel(&projects.Backend{Executor: scanBackend.Executor, BaseURL: scanBackend.BaseURL},
		&models.ProjectResponseModel{ID: "p1", RepoURL: "g", MainBranch: "main"})
	body := []byte("zip")
	_, err := scans.NewInvoker(scanBackend, repo, client).
		WithEngines("sast").
		ForBranch("feature").
		WithUploadReader(bytes.NewReader(body), int64(len(body))).
		Start(context.Background())
	var ce *cxerrors.ConfigurationError
	if !errors.As(err, &ce) || ce.Field != "Source" {
		t.Errorf("got %v", err)
	}
}

func TestInvokerStartUploadMode(t *testing.T) {
	// Two http servers: one for the API + IAM, one for the presigned-URL PUT.
	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("upload server method = %q", r.Method)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization should NOT be sent to presigned URL")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "zipdata" {
			t.Errorf("body = %q", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upSrv.Close()

	var createBody models.Scan
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/uploads":
			_, _ = io.WriteString(w, `{"url":"`+upSrv.URL+`/u/abc"}`)
		case "/api/scans":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"u-scan","status":"Queued"}`)
		}
	}))
	defer apiSrv.Close()

	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    apiSrv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test",
		RetryPolicy:   retry.Policy{MaxAttempts: 1},
	})
	scanBackend := &scans.Backend{Executor: exec, BaseURL: apiSrv.URL + "/"}
	projBackend := &projects.Backend{Executor: exec, BaseURL: apiSrv.URL + "/"}

	repo, _ := projects.FromProjectModel(projBackend, &models.ProjectResponseModel{
		ID: "p1", RepoURL: "git@x", MainBranch: "main",
	})

	body := []byte("zipdata")
	insp, err := scans.NewInvoker(scanBackend, repo, apiSrv.Client()).
		WithEngines("sast").
		WithUploadReader(bytes.NewReader(body), int64(len(body))).
		Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if insp.ID() != "u-scan" {
		t.Errorf("got id %q", insp.ID())
	}
	if createBody.Type != "upload" {
		t.Errorf("type = %q", createBody.Type)
	}
	var h models.ScanHandler
	_ = json.Unmarshal(createBody.Handler, &h)
	if !strings.HasPrefix(h.UploadURL, upSrv.URL) {
		t.Errorf("uploadUrl = %q", h.UploadURL)
	}
}

func TestInvokerSastConfigPropagatesIntoEngineConfig(t *testing.T) {
	var createBody models.Scan
	scanBackend, _, client := newScanBackends(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &createBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"s","status":"Queued"}`)
	})
	repo, _ := projects.FromProjectModel(&projects.Backend{Executor: scanBackend.Executor, BaseURL: scanBackend.BaseURL},
		&models.ProjectResponseModel{ID: "p1", RepoURL: "g", MainBranch: "main"})
	_, err := scans.NewInvoker(scanBackend, repo, client).
		WithEngines("sast").
		WithSastConfig(models.SastConfig{PresetName: "ASA Premium", Incremental: "true"}).
		Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(createBody.Config) != 1 || createBody.Config[0].Type != "sast" {
		t.Fatalf("config = %+v", createBody.Config)
	}
	if got, _ := createBody.Config[0].Value["presetName"].(string); got != "ASA Premium" {
		t.Errorf("presetName = %v", createBody.Config[0].Value)
	}
}
