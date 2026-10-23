package aitriage_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/aitriage"
)

// CONFIRMED is returned by the live service (S1 policy run) although the
// spec's TriageStatus enum lacks it: it is terminal and known.
func TestInspectorConfirmedIsDone(t *testing.T) {
	b := backend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"resultID":"r","triageStatus":"CONFIRMED"}`)
	})
	in, err := aitriage.GetResult(context.Background(), b, "p", "g")
	if err != nil {
		t.Fatal(err)
	}
	if !in.Done() || in.Failed() || in.Unknown() || in.Status() != aitriage.StatusConfirmed {
		t.Fatalf("done=%v failed=%v unknown=%v status=%s", in.Done(), in.Failed(), in.Unknown(), in.Status())
	}
}

// RawDetail returns the detail body byte for byte: false/empty values and
// unknown fields that the typed projection drops survive.
func TestInspectorRawDetailIsVerbatim(t *testing.T) {
	body := `{"resultID":"r","triageStatus":"CONFIRMED","mockOrigin":false,"summary":"","brandNew":{"x":1}}`
	b := backend(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
	in, err := aitriage.GetResult(context.Background(), b, "p", "g")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(in.RawDetail()); got != body {
		t.Fatalf("RawDetail = %s\nwant      %s", got, body)
	}
}

func TestInspectorRawDetailNilForJobShape(t *testing.T) {
	b := backend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"projectID":"p","groupID":"g","jobStatus":"IN_PROGRESS"}`)
	})
	in, err := aitriage.GetResult(context.Background(), b, "p", "g")
	if err != nil {
		t.Fatal(err)
	}
	if in.RawDetail() != nil {
		t.Fatalf("RawDetail = %s, want nil", in.RawDetail())
	}
}
