package policies_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/policies"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newBackend(t *testing.T, h http.HandlerFunc) *policies.Backend {
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
	return &policies.Backend{Executor: exec, BaseURL: srv.URL + "/"}
}

func TestEvaluateReturnsBreakBuild(t *testing.T) {
	var gotScan string
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotScan = r.URL.Query().Get("scan-id")
		_, _ = io.WriteString(w, `{"status":"VIOLATED","breakBuild":true,"policies":[{"policyName":"p1","breakBuild":true,"status":"VIOLATED"}]}`)
	})
	got, err := policies.Evaluate(context.Background(), b, "scan-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.BreakBuild || got.Status != "VIOLATED" || len(got.Policies) != 1 {
		t.Fatalf("got %+v", got)
	}
	if gotScan != "scan-1" {
		t.Fatalf("scan-id = %q", gotScan)
	}
}

func TestEvaluateNoPolicies(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"COMPLETED","breakBuild":false,"policies":[]}`)
	})
	got, err := policies.Evaluate(context.Background(), b, "scan-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.BreakBuild || len(got.Policies) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestEvaluateValidatesInput(t *testing.T) {
	var ce *cxerrors.ConfigurationError
	if _, err := policies.Evaluate(context.Background(), nil, "s"); !errors.As(err, &ce) {
		t.Fatalf("nil backend err = %v", err)
	}
	b := newBackend(t, func(http.ResponseWriter, *http.Request) {})
	if _, err := policies.Evaluate(context.Background(), b, ""); !errors.As(err, &ce) {
		t.Fatalf("empty scanID err = %v", err)
	}
}
