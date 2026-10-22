package scans_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/projects"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scans"
)

// captureScan returns an invoker factory whose POST /api/scans body is captured raw.
func captureScan(t *testing.T) (func() *scans.Invoker, *string, *int32) {
	t.Helper()
	var raw string
	var hits int32
	b, pb, client := newScanBackends(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"s","status":"Queued"}`)
	})
	repo, _ := projects.FromProjectModel(pb, &models.ProjectResponseModel{ID: "p1", RepoURL: "https://x/y.git", MainBranch: "main"})
	return func() *scans.Invoker { return scans.NewInvoker(b, repo, client) }, &raw, &hits
}

func wantConfigErr(t *testing.T, err error, hits *int32) {
	t.Helper()
	var ce *cxerrors.ConfigurationError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigurationError, got %v", err)
	}
	if atomic.LoadInt32(hits) != 0 {
		t.Error("HTTP was issued despite validation failure")
	}
}

func TestWithConfigsBodyMatchesSpecExample(t *testing.T) {
	mk, raw, _ := captureScan(t)
	_, err := mk().ForBranch("main").WithConfigs(
		models.Config{Type: "sast", Value: map[string]interface{}{"presetName": "Checkmarx Default", "incremental": "false"}},
		models.Config{Type: "microengines", Value: map[string]interface{}{"2ms": "true", "scorecard": "false"}},
	).Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Config  []models.Config `json:"config"`
		Project map[string]any  `json:"project"`
	}
	if err := json.Unmarshal([]byte(*raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Config) != 2 || got.Config[0].Type != "sast" || got.Config[1].Value["2ms"] != "true" {
		t.Errorf("config = %+v", got.Config)
	}
}

func TestWithConfigsNonStringValueFailsBeforeHTTP(t *testing.T) {
	mk, _, hits := captureScan(t)
	_, err := mk().ForBranch("main").WithConfigs(
		models.Config{Type: "sca", Value: map[string]interface{}{"exploitablePath": true}},
	).Start(context.Background())
	wantConfigErr(t, err, hits)
}

func TestWithConfigsRejectsDuplicatesAndUnknownTypes(t *testing.T) {
	mk, _, hits := captureScan(t)
	_, err := mk().ForBranch("main").WithConfigs(
		models.Config{Type: "sast"}, models.Config{Type: "sast"},
	).Start(context.Background())
	wantConfigErr(t, err, hits)
	_, err = mk().ForBranch("main").WithConfigs(models.Config{Type: "bogus"}).Start(context.Background())
	wantConfigErr(t, err, hits)
	// Duplicate across WithEngines + WithConfigs.
	_, err = mk().ForBranch("main").WithEngines("sast").WithConfigs(models.Config{Type: "sast"}).Start(context.Background())
	wantConfigErr(t, err, hits)
}

func TestWithEnginesAllowListFollowsSpecEnum(t *testing.T) {
	for _, e := range []string{"sast", "sca", "kics", "apisec", "containers", "microengines", "aisc"} {
		mk, raw, _ := captureScan(t)
		if _, err := mk().ForBranch("main").WithEngines(e).Start(context.Background()); err != nil {
			t.Errorf("%s: %v", e, err)
		}
		if !strings.Contains(*raw, `"type":"`+e+`"`) {
			t.Errorf("%s: body %s", e, *raw)
		}
	}
	mk, _, hits := captureScan(t)
	_, err := mk().ForBranch("main").WithEngines("sast", "nope").Start(context.Background())
	wantConfigErr(t, err, hits)
}

func TestWithEnginesScsIsDeprecatedAliasForMicroengines(t *testing.T) {
	mk, raw, _ := captureScan(t)
	if _, err := mk().ForBranch("main").WithEngines("scs").WithSCSConfig(models.SCSConfig{Twoms: "true"}).Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(*raw, `"scs"`) || !strings.Contains(*raw, `"type":"microengines"`) || !strings.Contains(*raw, `"2ms":"true"`) {
		t.Errorf("body = %s", *raw)
	}
	// scs + microengines is a duplicate after aliasing.
	mk, _, hits := captureScan(t)
	_, err := mk().ForBranch("main").WithEngines("scs", "microengines").Start(context.Background())
	wantConfigErr(t, err, hits)
}

func TestProjectTagsOmittedUnlessWithProjectTags(t *testing.T) {
	mk, raw, _ := captureScan(t)
	if _, err := mk().ForBranch("main").WithEngines("sast").WithTags(map[string]string{"scan": "t"}).Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var generic map[string]json.RawMessage
	_ = json.Unmarshal([]byte(*raw), &generic)
	var proj map[string]json.RawMessage
	_ = json.Unmarshal(generic["project"], &proj)
	if _, has := proj["tags"]; has {
		t.Errorf("project.tags must be omitted, body = %s", *raw)
	}
	if string(generic["tags"]) != `{"scan":"t"}` {
		t.Errorf("scan tags = %s", generic["tags"])
	}

	mk, raw, _ = captureScan(t)
	if _, err := mk().ForBranch("main").WithEngines("sast").WithProjectTags(map[string]string{"team": "a"}).Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(*raw), &generic)
	_ = json.Unmarshal(generic["project"], &proj)
	if string(proj["tags"]) != `{"team":"a"}` {
		t.Errorf("project tags = %s", proj["tags"])
	}
}

func uploadHarness(t *testing.T, putDelay time.Duration, clientTimeout time.Duration, handlerOut *string) (func() *scans.Invoker, *http.Client) {
	t.Helper()
	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(putDelay)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upSrv.Close)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/uploads":
			_, _ = io.WriteString(w, `{"url":"`+upSrv.URL+`/u/abc"}`)
		case "/api/scans":
			body, _ := io.ReadAll(r.Body)
			var s struct {
				Handler json.RawMessage `json:"handler"`
			}
			_ = json.Unmarshal(body, &s)
			if handlerOut != nil {
				*handlerOut = string(s.Handler)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"u","status":"Queued"}`)
		}
	}))
	t.Cleanup(apiSrv.Close)
	exec := transport.NewExecutor(transport.Config{
		HTTPClient: apiSrv.Client(), Authenticator: stubAuth{}, UserAgent: "test",
		RetryPolicy: retry.Policy{MaxAttempts: 1},
	})
	hc := &http.Client{Timeout: clientTimeout}
	sb := &scans.Backend{Executor: exec, BaseURL: apiSrv.URL + "/"}
	pb := &projects.Backend{Executor: exec, BaseURL: apiSrv.URL + "/"}
	repo, _ := projects.FromProjectModel(pb, &models.ProjectResponseModel{ID: "p1", MainBranch: "main"})
	return func() *scans.Invoker {
		return scans.NewInvoker(sb, repo, hc).WithEngines("sast").WithUploadReader(strings.NewReader("zip"), 3)
	}, hc
}

func TestWithUploadTimeoutSlowPUTSucceedsPastClientTimeout(t *testing.T) {
	mk, _ := uploadHarness(t, 300*time.Millisecond, 60*time.Millisecond, nil)
	// Without the option the client timeout caps the PUT.
	if _, err := mk().Start(context.Background()); err == nil {
		t.Fatal("expected client timeout to cap the PUT")
	}
	if _, err := mk().WithUploadTimeout(5 * time.Second).Start(context.Background()); err != nil {
		t.Fatalf("slow PUT should succeed: %v", err)
	}
}

func TestWithUploadTimeoutEnforcesItsOwnDeadline(t *testing.T) {
	mk, _ := uploadHarness(t, 400*time.Millisecond, 0, nil)
	_, err := mk().WithUploadTimeout(50 * time.Millisecond).Start(context.Background())
	if err == nil {
		t.Fatal("expected upload deadline error")
	}
	_, err = mk().WithUploadTimeout(-1).Start(context.Background())
	var ce *cxerrors.ConfigurationError
	if !errors.As(err, &ce) {
		t.Errorf("non-positive timeout should be ConfigurationError, got %v", err)
	}
}

func TestUploadHandlerOmitsEmptyRepoURLAndCredentials(t *testing.T) {
	var h string
	mk, _ := uploadHarness(t, 0, time.Second, &h)
	if _, err := mk().Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h, "repoUrl") || strings.Contains(h, "credentials") || strings.Contains(h, "uploadFormat") {
		t.Errorf("handler = %s", h)
	}
	if !strings.Contains(h, `"uploadUrl"`) {
		t.Errorf("handler = %s", h)
	}
	mk, _ = uploadHarness(t, 0, time.Second, &h)
	if _, err := mk().WithUploadFormat("single").Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h, `"uploadFormat":"single"`) {
		t.Errorf("handler = %s", h)
	}
}

func TestScanHandlerJSONKeepsCredentialsWhenSet(t *testing.T) {
	b, _ := json.Marshal(models.ScanHandler{RepoURL: "r", UploadURL: "u", Credentials: models.GitCredentials{Type: "apiKey", Value: "v"}})
	if !strings.Contains(string(b), `"credentials":{"type":"apiKey","value":"v"}`) || !strings.Contains(string(b), `"repoUrl":"r"`) {
		t.Errorf("got %s", b)
	}
}
