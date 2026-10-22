package aitriage_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/aitriage"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newExec(t *testing.T, h http.HandlerFunc) (*transport.Executor, string) {
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
	return exec, srv.URL + "/"
}

func TestSubmitSendsBodyAndDecodes202(t *testing.T) {
	var gotBody map[string]any
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/ai-triage/triage" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"scanID":"s1","status":"accepted","triageID":"t1","published":true,"existingTriageState":null,"newField":1}`)
	})
	out, err := aitriage.Submit(context.Background(), exec, base, &models.TriageRequest{
		ScanID:  "s1",
		Buckets: []models.TriageBucket{{ScannerType: "sast", ResultIDs: []string{"a", "b"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.TriageID != "t1" || !out.Published || out.ExistingTriageState != nil {
		t.Errorf("got %+v", out)
	}
	if gotBody["scanID"] != "s1" {
		t.Errorf("body %v", gotBody)
	}
	b := gotBody["buckets"].([]any)[0].(map[string]any)
	if b["scannerType"] != "sast" || len(b["resultIDs"].([]any)) != 2 {
		t.Errorf("bucket %v", b)
	}
}

func TestSubmitPublishedFalseIsSuccess(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"scanID":"s1","status":"accepted","triageID":"t1","published":false,"existingTriageState":"IN_PROGRESS"}`)
	})
	out, err := aitriage.Submit(context.Background(), exec, base, &models.TriageRequest{ScanID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Published || out.ExistingTriageState == nil || *out.ExistingTriageState != "IN_PROGRESS" {
		t.Errorf("got %+v", out)
	}
}

func TestSubmitPublishedDefaultsTrueWhenAbsent(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"scanID":"s1","status":"accepted","triageID":"t1"}`)
	})
	out, err := aitriage.Submit(context.Background(), exec, base, &models.TriageRequest{ScanID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Published {
		t.Errorf("published should default true")
	}
}

func TestSubmitTypedErrors(t *testing.T) {
	cases := []struct {
		status int
		kind   cxerrors.TriageErrorKind
	}{
		{400, cxerrors.TriageSourceUnavailable},
		{402, cxerrors.TriageInsufficientCredits},
		{403, cxerrors.TriageForbidden},
	}
	for _, c := range cases {
		exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, `{"error":"x","message":"nope Bearer abc.def.ghi"}`)
		})
		_, err := aitriage.Submit(context.Background(), exec, base, &models.TriageRequest{ScanID: "s1"})
		var te *cxerrors.TriageError
		if !errors.As(err, &te) {
			t.Fatalf("status %d: not TriageError: %v", c.status, err)
		}
		if te.Kind != c.kind || te.StatusCode != c.status {
			t.Errorf("status %d: got %+v", c.status, te)
		}
		if strings.Contains(err.Error(), "abc.def.ghi") {
			t.Errorf("token leaked: %v", err)
		}
		var re *cxerrors.ResponseError
		if !errors.As(err, &re) {
			t.Errorf("underlying ResponseError should remain reachable")
		}
	}
}

func TestSubmitOtherStatusStaysResponseError(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(422) })
	_, err := aitriage.Submit(context.Background(), exec, base, &models.TriageRequest{ScanID: "s1"})
	var te *cxerrors.TriageError
	if errors.As(err, &te) {
		t.Errorf("422 should not be TriageError")
	}
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) {
		t.Errorf("want ResponseError, got %v", err)
	}
}

func TestGetResultInProgress(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/ai-triage/triage/p1/123%23-%23pkg" {
			t.Errorf("path %q", r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"projectID":"p1","groupID":"g","jobStatus":"IN_PROGRESS","extra":true}`)
	})
	env, err := aitriage.GetResult(context.Background(), exec, base, "p1", "123#-#pkg")
	if err != nil {
		t.Fatal(err)
	}
	if env.JobStatus != "IN_PROGRESS" || env.Detail != nil || env.ProjectID != "p1" {
		t.Errorf("got %+v", env)
	}
}

func TestGetResultFailedJob(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"projectID":"p1","groupID":"g","jobStatus":"FAILED"}`)
	})
	env, err := aitriage.GetResult(context.Background(), exec, base, "p1", "g")
	if err != nil {
		t.Fatal(err)
	}
	if env.JobStatus != "FAILED" || env.Detail != nil {
		t.Errorf("got %+v", env)
	}
}

func TestGetResultDetail(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"resultID":"r1","scanner":"sast","triageStatus":"PROPOSED_NOT_EXPLOITABLE",
"reachabilityStatus":"NOT_REACHABLE","exploitabilityStatus":"NOT_EXPLOITABLE","attackabilityStatus":"NOT_ATTACKABLE",
"summary":"s","triagedAt":"2026-06-15T13:38:17Z",
"analysis":{"confidence":{"score":0.9,"explanation":"e"},"reachability":{"status":"NOT_REACHABLE","reasoning":"r"},"exploitability":{"status":"NOT_EXPLOITABLE"},"usage_locations":["a.go"]},
"metadata":{"component":"lib","version":null},
"reasoningTrace":{"verification_steps":[{"task":"t","category":"REACHABILITY","status":"VERIFIED","thoughts":["x"]}],"repository_info":null},
"mockOrigin":false,"groupId":"g","projectId":"p1","sourceProjectId":null,"unknownFuture":{"a":1}}`)
	})
	env, err := aitriage.GetResult(context.Background(), exec, base, "p1", "g")
	if err != nil {
		t.Fatal(err)
	}
	d := env.Detail
	if d == nil || env.JobStatus != "" {
		t.Fatalf("got %+v", env)
	}
	if d.TriageStatus != "PROPOSED_NOT_EXPLOITABLE" || d.Analysis.Confidence.Score != 0.9 ||
		d.ExploitabilityStatus != "NOT_EXPLOITABLE" || d.GroupID == nil || *d.GroupID != "g" ||
		len(d.ReasoningTrace.VerificationSteps) != 1 || d.TriagedAt == nil || d.TriagedAt.IsZero() {
		t.Errorf("detail %+v", d)
	}
}

func TestGetResultTypedErrors403(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	_, err := aitriage.GetResult(context.Background(), exec, base, "p1", "g")
	var te *cxerrors.TriageError
	if !errors.As(err, &te) || te.Kind != cxerrors.TriageForbidden {
		t.Errorf("got %v", err)
	}
}

func TestGetResult404StaysResponseError(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	_, err := aitriage.GetResult(context.Background(), exec, base, "p1", "g")
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 404 {
		t.Errorf("got %v", err)
	}
}

func TestSubmitEmptyBodyDefaultsPublishedTrue(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	out, err := aitriage.Submit(context.Background(), exec, base, &models.TriageRequest{ScanID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Published {
		t.Errorf("empty 202 body must default published=true: %+v", out)
	}
}

func TestGetResultMalformedBodies(t *testing.T) {
	for name, body := range map[string]string{
		"empty":            ``,
		"null":             `null`,
		"empty object":     `{}`,
		"null jobStatus":   `{"projectID":"p","groupID":"g","jobStatus":null}`,
		"empty jobStatus":  `{"projectID":"p","groupID":"g","jobStatus":""}`,
		"unrelated fields": `{"foo":"bar"}`,
	} {
		exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
		env, err := aitriage.GetResult(context.Background(), exec, base, "p", "g")
		var re *cxerrors.ResponseError
		if !errors.As(err, &re) || !errors.Is(err, cxerrors.ErrResponse) {
			t.Errorf("%s: want ResponseError, got env=%+v err=%v", name, env, err)
		}
		var te *cxerrors.TriageError
		if errors.As(err, &te) {
			t.Errorf("%s: must not be TriageError", name)
		}
	}
}

func TestGetResultDetailInProgressTriageStatus(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"triageStatus":"IN_PROGRESS"}`)
	})
	env, err := aitriage.GetResult(context.Background(), exec, base, "p", "g")
	if err != nil || env.Detail == nil || env.Detail.TriageStatus != "IN_PROGRESS" {
		t.Fatalf("got %+v %v", env, err)
	}
}

func TestGetResultTriagedAtTolerant(t *testing.T) {
	for name, v := range map[string]string{"empty": `""`, "bad": `"yesterday"`, "null": `null`, "number": `5`} {
		exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"resultID":"r","triageStatus":"VULNERABLE","triagedAt":`+v+`}`)
		})
		env, err := aitriage.GetResult(context.Background(), exec, base, "p", "g")
		if err != nil || env.Detail == nil || env.Detail.TriageStatus != "VULNERABLE" || env.Detail.TriagedAt != nil {
			t.Errorf("%s: got %+v %v", name, env, err)
		}
	}
}

func TestGetResult400And402StayResponseError(t *testing.T) {
	for _, code := range []int{400, 402} {
		exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		_, err := aitriage.GetResult(context.Background(), exec, base, "p1", "g")
		var re *cxerrors.ResponseError
		var te *cxerrors.TriageError
		if !errors.As(err, &re) || re.StatusCode != code || errors.As(err, &te) {
			t.Errorf("%d: got %v", code, err)
		}
	}
}

func TestGetResultSlashInGroupIDEscaped(t *testing.T) {
	var uri string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		uri = r.RequestURI
		_, _ = io.WriteString(w, `{"projectID":"p","groupID":"a/b","jobStatus":"IN_PROGRESS"}`)
	})
	if _, err := aitriage.GetResult(context.Background(), exec, base, "p", "a/b#c"); err != nil {
		t.Fatal(err)
	}
	if uri != "/api/ai-triage/triage/p/a%2Fb%23c" {
		t.Errorf("RequestURI %q", uri)
	}
}
