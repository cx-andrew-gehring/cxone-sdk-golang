package aitriage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/aitriage"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func backend(t *testing.T, h http.HandlerFunc) *aitriage.Backend {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &aitriage.Backend{
		Executor: transport.NewExecutor(transport.Config{
			HTTPClient: srv.Client(), Authenticator: stubAuth{}, UserAgent: "t", CorrelationID: "c",
			RetryPolicy: retry.Policy{MaxAttempts: 1},
		}),
		BaseURL: srv.URL + "/",
	}
}

func TestInspectorStates(t *testing.T) {
	cases := []struct {
		name, body                       string
		done, failed, unknown, hasDetail bool
		status                           string
	}{
		{"job in progress", `{"projectID":"p","groupID":"g","jobStatus":"IN_PROGRESS"}`, false, false, false, false, "IN_PROGRESS"},
		{"job failed", `{"projectID":"p","groupID":"g","jobStatus":"FAILED"}`, true, true, false, false, "FAILED"},
		{"job not triaged", `{"projectID":"p","groupID":"g","jobStatus":"NOT_TRIAGED"}`, false, false, false, false, "NOT_TRIAGED"},
		{"job unknown", `{"projectID":"p","groupID":"g","jobStatus":"WEIRD"}`, false, false, true, false, "WEIRD"},
		{"detail vulnerable", `{"resultID":"r","triageStatus":"VULNERABLE","triagedAt":"2026-06-15T13:38:17Z"}`, true, false, false, true, "VULNERABLE"},
		{"detail not exploitable", `{"resultID":"r","triageStatus":"PROPOSED_NOT_EXPLOITABLE"}`, true, false, false, true, "PROPOSED_NOT_EXPLOITABLE"},
		{"detail uncertain", `{"resultID":"r","triageStatus":"UNCERTAIN"}`, true, false, false, true, "UNCERTAIN"},
		{"detail risk accepted", `{"resultID":"r","triageStatus":"RISK_ACCEPTED"}`, true, false, false, true, "RISK_ACCEPTED"},
		{"detail failed", `{"resultID":"r","triageStatus":"FAILED"}`, true, true, false, true, "FAILED"},
		{"detail in progress", `{"resultID":"r","triageStatus":"IN_PROGRESS"}`, false, false, false, true, "IN_PROGRESS"},
		{"detail not triaged", `{"resultID":"r","triageStatus":"NOT_TRIAGED"}`, false, false, false, true, "NOT_TRIAGED"},
		{"detail unknown", `{"resultID":"r","triageStatus":"NEW_THING"}`, false, false, true, true, "NEW_THING"},
	}
	for _, c := range cases {
		b := backend(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, c.body) })
		in, err := aitriage.GetResult(context.Background(), b, "p", "g")
		if err != nil {
			t.Fatal(c.name, err)
		}
		if in.Done() != c.done || in.Failed() != c.failed || in.Unknown() != c.unknown ||
			(in.Detail() != nil) != c.hasDetail || in.Status() != c.status {
			t.Errorf("%s: done=%v failed=%v unknown=%v detail=%v status=%q", c.name, in.Done(), in.Failed(), in.Unknown(), in.Detail(), in.Status())
		}
	}
}

func TestGetResultMalformedIsResponseError(t *testing.T) {
	b := backend(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{}`) })
	in, err := aitriage.GetResult(context.Background(), b, "p", "g")
	if in != nil || !errors.Is(err, cxerrors.ErrResponse) {
		t.Errorf("got %v %v", in, err)
	}
}

func TestIsNotFound(t *testing.T) {
	b := backend(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	_, err := aitriage.GetResult(context.Background(), b, "p", "g")
	if !aitriage.IsNotFound(err) {
		t.Errorf("404 not detected: %v", err)
	}
	if aitriage.IsNotFound(nil) || aitriage.IsNotFound(errors.New("x")) || aitriage.IsNotFound(&cxerrors.ResponseError{StatusCode: 500}) {
		t.Error("false positive")
	}
	if !aitriage.IsNotFound(fmt.Errorf("wrap: %w", err)) {
		t.Error("wrapped 404 not detected")
	}
}

func TestSubmitValidatesInput(t *testing.T) {
	b := backend(t, func(w http.ResponseWriter, r *http.Request) { t.Error("no request expected") })
	var ce *cxerrors.ConfigurationError
	if _, err := aitriage.Submit(context.Background(), b, "", models.TriageBucket{ScannerType: "sast"}); err == nil || !asConfig(err, &ce) {
		t.Errorf("empty scanID: %v", err)
	}
	if _, err := aitriage.Submit(context.Background(), b, "s"); err == nil || !asConfig(err, &ce) {
		t.Errorf("no buckets: %v", err)
	}
}

func TestSubmitOK(t *testing.T) {
	b := backend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		_, _ = io.WriteString(w, `{"scanID":"s","status":"accepted","triageID":"t","published":false}`)
	})
	out, err := aitriage.Submit(context.Background(), b, "s", models.TriageBucket{ScannerType: "sca", ResultIDs: []string{"x"}})
	if err != nil || out.TriageID != "t" || out.Published {
		t.Errorf("%+v %v", out, err)
	}
}

func asConfig(err error, target **cxerrors.ConfigurationError) bool {
	ce, ok := err.(*cxerrors.ConfigurationError)
	*target = ce
	return ok
}
