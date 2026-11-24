package cxone_test

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestClientPoliciesEvaluateBreakBuild(t *testing.T) {
	var gotScan string
	c := newWorkflowClient(t, map[string]http.HandlerFunc{
		"/api/policy_management_service_uri/evaluation": func(w http.ResponseWriter, r *http.Request) {
			gotScan = r.URL.Query().Get("scan-id")
			_, _ = io.WriteString(w, `{"status":"VIOLATED","breakBuild":true,"policies":[{"policyName":"p1","breakBuild":true}]}`)
		},
	})
	got, err := c.Policies().Evaluate(context.Background(), "scan-9")
	if err != nil {
		t.Fatal(err)
	}
	if !got.BreakBuild || gotScan != "scan-9" {
		t.Fatalf("got %+v scan=%q", got, gotScan)
	}
}
