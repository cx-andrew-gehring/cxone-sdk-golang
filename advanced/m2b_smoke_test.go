package advanced_test

// Smoke tests for the M2b endpoint packages. Each test exercises one happy
// path through the full stack: builder → IAM auth → executor → endpoint pkg
// → JSON decode. The patterns are exercised in detail in the per-package
// tests for projects/scans/uploads (M2); these tests confirm the wiring is
// right and the URL paths match what ast-cli sends.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// newClient builds a real cxone.Client wired against an httptest mux that
// handles the IAM token endpoint plus whatever API routes the test
// registers via routes.
func newClient(t *testing.T, routes map[string]http.HandlerFunc) *cxone.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/realms/acme/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok","token_type":"Bearer","expires_in":300}`)
	})
	for path, handler := range routes {
		mux.HandleFunc(path, handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "http://")
	region, err := cxone.NewCustomRegion(host, host)
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.WithScheme("http")
	if err != nil {
		t.Fatal(err)
	}
	c, err := cxone.NewClient().
		Region(region).
		Tenant("acme").
		AgentName("test-agent").
		APIKey("dummy").
		HTTPClient(srv.Client()).
		// Tests use the funnel for routing/header logic, not for retry coverage
		// (that lives in internal/transport tests). Disable backoff so a test
		// that intentionally returns 5xx doesn't sleep through the default
		// 15-second jitter.
		Retries(1).
		RetryDelaySeconds(0).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func TestAdvancedResults(t *testing.T) {
	var gotQuery url.Values
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/results": func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			writeJSON(w, `{"results":[{"id":"r1","severity":"high"}],"totalCount":1,"scanID":"s1"}`)
		},
	})
	out, err := c.Advanced().Results().List(context.Background(), url.Values{"scan-id": {"s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalCount != 1 || out.ScanID != "s1" {
		t.Errorf("got %+v", out)
	}
	if got := gotQuery.Get("scan-id"); got != "s1" {
		t.Errorf("scan-id = %q", got)
	}
}

func TestAdvancedReportsCreateAndPoll(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/reports/json": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			writeJSON(w, `{"reportId":"r1"}`)
		},
		"/api/reports/json/r1": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("return-url") != "true" {
				t.Errorf("return-url not set")
			}
			writeJSON(w, `{"reportId":"r1","status":"completed","url":"https://x/r1.json"}`)
		},
	})
	create, err := c.Advanced().Reports().CreateJSON(context.Background(), &models.JSONReportRequest{
		ReportType: "sast", ReportName: "demo", FileFormat: "json",
		Data: models.ReportData{ScanID: "s1", ProjectID: "p1", BranchName: "main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if create.ReportID != "r1" {
		t.Errorf("reportId = %q", create.ReportID)
	}
	poll, err := c.Advanced().Reports().PollJSON(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if poll.Status != models.ReportStatusCompleted {
		t.Errorf("status = %q", poll.Status)
	}
}

func TestAdvancedReportsDownloadJSONStream(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/reports/json/r1/download": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "binary-or-json-blob")
		},
	})
	rc, err := c.Advanced().Reports().DownloadJSON(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "binary-or-json-blob" {
		t.Errorf("body = %q", body)
	}
}

func TestAdvancedPoliciesEvaluate(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/policy_management_service_uri/evaluation": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("scan-id"); got != "s1" {
				t.Errorf("scan-id = %q", got)
			}
			writeJSON(w, `{"status":"OK","breakBuild":false,"policies":[]}`)
		},
	})
	out, err := c.Advanced().Policies().Evaluate(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "OK" {
		t.Errorf("status = %q", out.Status)
	}
}

func TestAdvancedGroupsSubstitutesTenantInPath(t *testing.T) {
	var gotPath string
	c := newClient(t, map[string]http.HandlerFunc{
		"/auth/realms/acme/pip/groups": func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			if got := r.URL.Query().Get("groupName"); got != "admins" {
				t.Errorf("groupName = %q", got)
			}
			writeJSON(w, `[{"id":"g1","name":"admins"}]`)
		},
	})
	out, err := c.Advanced().Groups().List(context.Background(), "admins")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "admins" {
		t.Errorf("got %+v", out)
	}
	if gotPath != "/auth/realms/acme/pip/groups" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestAdvancedAccessEntitiesFor(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/access-management/entities-for": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("resource-id") != "p1" || q.Get("resource-type") != "project" {
				t.Errorf("query = %v", q)
			}
			writeJSON(w, `[{"entityID":"u1","entityType":"user","entityName":"alice","resourceID":"p1","resourceType":"project","resourceName":"acme"}]`)
		},
	})
	out, err := c.Advanced().Access().EntitiesFor(context.Background(), "p1", "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].EntityName != "alice" {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedApplicationsList(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/applications": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"totalCount":1,"filteredTotalCount":1,"applications":[{"id":"a1","name":"acme","criticality":3}]}`)
		},
	})
	out, err := c.Advanced().Applications().List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Applications) != 1 || out.Applications[0].Criticality != 3 {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedFeatureFlagsList(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/flags": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("filter"); got != "tenant-uuid" {
				t.Errorf("filter = %q", got)
			}
			writeJSON(w, `[{"name":"FF1","status":true},{"name":"FF2","status":false}]`)
		},
	})
	out, err := c.Advanced().FeatureFlags().List(context.Background(), "tenant-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || !out[0].Status {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedSastMetadataList(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/sast-metadata": func(w http.ResponseWriter, r *http.Request) {
			ids := r.URL.Query()["scan-ids"]
			if len(ids) != 2 || ids[0] != "s1" || ids[1] != "s2" {
				t.Errorf("scan-ids = %v", ids)
			}
			writeJSON(w, `{"totalCount":2,"scans":[{"scanId":"s1","loc":1234},{"scanId":"s2","loc":5678}]}`)
		},
	})
	out, err := c.Advanced().SastMetadata().List(context.Background(), "s1", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Scans) != 2 || out.Scans[1].Loc != 5678 {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedTenantConfiguration(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/configuration/tenant": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `[{"key":"scan.timeout","value":"60"},{"key":"sast.preset","value":"ASA"}]`)
		},
	})
	out, err := c.Advanced().Tenant().GetConfiguration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1].Value != "ASA" {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedLogsReturnsPlainText(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/logs/s1/sast": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "engine started\nengine completed\n")
		},
	})
	out, err := c.Advanced().Logs().Get(context.Background(), "s1", "sast")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "engine started") {
		t.Errorf("body = %q", out)
	}
}

func TestAdvancedBFL(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/bfl": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("scan-id") != "s1" || q.Get("query-id") != "q1" {
				t.Errorf("query = %v", q)
			}
			writeJSON(w, `{"id":"b1","trees":[],"totalCount":0}`)
		},
	})
	out, err := c.Advanced().BFL().Get(context.Background(), "s1", "q1")
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "b1" {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedPredicatesGetSAST(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/sast-results-predicates/sim123": func(w http.ResponseWriter, r *http.Request) {
			ids := r.URL.Query()["project-ids"]
			if len(ids) != 1 || ids[0] != "p1" {
				t.Errorf("project-ids = %v", ids)
			}
			writeJSON(w, `{"predicateHistoryPerProject":[{"projectId":"p1","similarityId":"sim123","predicates":[],"totalCount":0}],"totalCount":1}`)
		},
	})
	out, err := c.Advanced().Predicates().GetSAST(context.Background(), "sim123", []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalCount != 1 {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedPredicatesUpdateSAST(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/sast-results-predicates": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %q", r.Method)
			}
			w.WriteHeader(http.StatusOK)
		},
	})
	state := models.PredicateStateNotExploitable
	err := c.Advanced().Predicates().UpdateSAST(context.Background(), []models.PredicateRequest{
		{SimilarityID: "sim", ProjectID: "p1", State: &state, Comment: "fp", Severity: "high"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdvancedPredicatesCustomStates(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/custom-states": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("include-deleted"); got != "true" {
				t.Errorf("include-deleted = %q", got)
			}
			writeJSON(w, `[{"id":1,"name":"InReview","type":"sast"}]`)
		},
	})
	out, err := c.Advanced().Predicates().ListCustomStates(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "InReview" {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedExportCreateAndPoll(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/sca/export/requests": func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				w.WriteHeader(http.StatusAccepted)
				writeJSON(w, `{"exportId":"e1"}`)
			case http.MethodGet:
				if got := r.URL.Query().Get("export-id"); got != "e1" {
					t.Errorf("export-id = %q", got)
				}
				writeJSON(w, `{"exportId":"e1","exportStatus":"Completed","fileUrl":"https://x"}`)
			}
		},
	})
	create, err := c.Advanced().Export().Create(context.Background(), &models.ExportRequestPayload{
		ScanID: "s1", FileFormat: "CycloneDxJson",
	})
	if err != nil {
		t.Fatal(err)
	}
	if create.ExportID != "e1" {
		t.Errorf("exportId = %q", create.ExportID)
	}
	poll, err := c.Advanced().Export().Poll(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}
	if poll.ExportStatus != "Completed" {
		t.Errorf("status = %q", poll.ExportStatus)
	}
}

func TestAdvancedScanOverview(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/micro-engines/read/scans/s1/scan-overview": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"status":"Completed","totalRisks":3,"riskSummary":{"high":2,"medium":1}}`)
		},
	})
	out, err := c.Advanced().ScanOverview().Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalRisksCount != 3 || out.Status != models.ScanCompleted {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedRisksManagement(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/risk-management/projects/p1/results": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("scanID"); got != "s1" {
				t.Errorf("scanID = %q", got)
			}
			writeJSON(w, `{"projectID":"p1","scanID":"s1","applicationNameIDMap":[],"results":[]}`)
		},
	})
	out, err := c.Advanced().Risks().GetRiskManagement(context.Background(), "p1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if out.ProjectID != "p1" || out.ScanID != "s1" {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedRisksAPISecOverview(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/apisec/static/api/scan/s1/risks-overview": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"api_count":12,"total_risks_count":4,"risks":[1,2,3]}`)
		},
	})
	out, err := c.Advanced().Risks().GetAPISecOverview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if out.APICount != 12 || out.TotalRisksCount != 4 {
		t.Errorf("got %+v", out)
	}
}

func TestAdvancedTelemetry(t *testing.T) {
	var gotBody string
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/telemetry/log": func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
		},
	})
	err := c.Advanced().Telemetry().LogAIEvent(context.Background(), &models.AITelemetryEvent{
		AIProvider: "anthropic", Type: "test", TotalCount: 1, UniqueID: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"aiProvider":"anthropic"`) {
		t.Errorf("body = %q", gotBody)
	}
}

// Sanity check that a 404 from a new endpoint package surfaces as
// *cxerrors.ResponseError (not silently swallowed).
func TestAdvancedReportsPollMissing(t *testing.T) {
	c := newClient(t, map[string]http.HandlerFunc{
		"/api/reports/json/missing": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, `{"message":"report not found","code":404}`)
		},
	})
	_, err := c.Advanced().Reports().PollJSON(context.Background(), "missing")
	var rerr *cxerrors.ResponseError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *ResponseError, got %T: %v", err, err)
	}
	if rerr.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", rerr.StatusCode)
	}
}
