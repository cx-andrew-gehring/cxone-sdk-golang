package aitriage_test

import (
	"context"
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
		name, body        string
		done, failed, det bool
	}{
		{"in progress", `{"projectID":"p","groupID":"g","jobStatus":"IN_PROGRESS"}`, false, false, false},
		{"failed", `{"projectID":"p","groupID":"g","jobStatus":"FAILED"}`, true, true, false},
		{"detail", `{"resultID":"r","scanner":"sast","triageStatus":"VULNERABLE","triagedAt":"2026-06-15T13:38:17Z"}`, true, false, true},
		{"detail failed triageStatus", `{"resultID":"r","scanner":"sast","triageStatus":"FAILED"}`, true, true, true},
	}
	for _, c := range cases {
		b := backend(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, c.body) })
		in, err := aitriage.GetResult(context.Background(), b, "p", "g")
		if err != nil {
			t.Fatal(c.name, err)
		}
		if in.Done() != c.done || in.Failed() != c.failed || (in.Detail() != nil) != c.det {
			t.Errorf("%s: done=%v failed=%v detail=%v", c.name, in.Done(), in.Failed(), in.Detail())
		}
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
