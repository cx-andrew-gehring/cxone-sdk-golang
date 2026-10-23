package cxone_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/reports"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

// newWorkflowClient builds a real cxone.Client backed by an httptest mux
// that handles IAM auth + the routes the test registers.
func newWorkflowClient(t *testing.T, routes map[string]http.HandlerFunc) *cxone.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/realms/acme/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"tok","token_type":"Bearer","expires_in":300}`)
	})
	for p, h := range routes {
		mux.HandleFunc(p, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "http://")
	region, _ := cxone.NewCustomRegion(host, host)
	region, _ = region.WithScheme("http")
	c, err := cxone.NewClient().
		Region(region).
		Tenant("acme").
		AgentName("test-agent").
		APIKey("dummy").
		HTTPClient(srv.Client()).
		Retries(1).RetryDelaySeconds(0).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestWorkflowProjectsAndScansEndToEnd exercises the full M4 front door:
// load a project, build a scan via the fluent invoker, wait for it to
// complete via the polling waiter, then inspect the final state.
func TestWorkflowProjectsAndScansEndToEnd(t *testing.T) {
	var (
		scanGets int32
		gotScan  models.Scan
	)
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/projects/p1": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"id":"p1","name":"acme","mainBranch":"main","repoUrl":"https://example.com/acme.git"}`)
		},
		"/api/scans": func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotScan)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"s1","status":"Queued","engines":["sast"]}`)
		},
		"/api/scans/s1": func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&scanGets, 1)
			if n < 3 {
				_, _ = io.WriteString(w, `{"id":"s1","status":"Running","engines":["sast"]}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"s1","status":"Completed","engines":["sast"]}`)
		},
	})

	// Load project via the high-level workflow.
	repo, err := c.Projects().Get(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if repo.Name() != "acme" {
		t.Errorf("name = %q", repo.Name())
	}

	// Start a scan via the fluent invoker.
	insp, err := c.Scans().NewScan(repo).
		ForBranch("main").
		WithEngines("sast").
		WithSastConfig(models.SastConfig{PresetName: "Checkmarx Default"}).
		Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if insp.ID() != "s1" {
		t.Errorf("inspector id = %q", insp.ID())
	}
	if gotScan.Type != "git" {
		t.Errorf("scan type = %q", gotScan.Type)
	}

	// Wait for it to complete.
	final, err := scans.WaitUntilComplete(context.Background(), insp, scans.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Successful() {
		t.Errorf("expected Successful, got status=%q", final.Status())
	}
}

// TestWorkflowReportsEndToEnd exercises the report builder + waiter +
// download against a complete mock.
func TestWorkflowReportsEndToEnd(t *testing.T) {
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/reports/json": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"reportId":"rep1"}`)
		},
		"/api/reports/json/rep1": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"reportId":"rep1","status":"completed","url":"https://x"}`)
		},
		"/api/reports/json/rep1/download": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"finding":"x"}`)
		},
	})
	h, err := c.Reports().NewJSONReport().
		ForScan("s1", "p1", "main").
		WithSections("scan-summary").
		Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.ReportID() != "rep1" {
		t.Errorf("reportID = %q", h.ReportID())
	}
	var buf bytes.Buffer
	_, err = h.WaitAndDownload(context.Background(), &buf, reports.WaitOptions{PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "finding") {
		t.Errorf("download body = %q", buf.String())
	}
}

// TestWorkflowPresetsCachesAcrossCalls exercises the PresetReader's
// level-1 cache via the client front door.
func TestWorkflowPresetsCachesAcrossCalls(t *testing.T) {
	var hits int32
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/presets": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
			_, _ = io.WriteString(w, `{"presets":[{"id":"id-1","name":"P1"}]}`)
		},
	})
	pr := c.Presets()
	for i := 0; i < 5; i++ {
		all, err := pr.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 {
			t.Errorf("got %v", all)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 hit (cached), got %d", got)
	}
}

// TestProjectsHandleWrapSkipsHTTP verifies that wrapping a pre-fetched
// project model does not issue a network call.
func TestProjectsHandleWrapSkipsHTTP(t *testing.T) {
	var hits int32
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/projects/p1": func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	repo, err := c.Projects().Wrap(&models.ProjectResponseModel{
		ID: "p1", Name: "wrapped", MainBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.Name() != "wrapped" {
		t.Errorf("name = %q", repo.Name())
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("hits = %d, want 0", got)
	}
}
