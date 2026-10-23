package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
)

// A 429 is not retried (CLAUDE.md §4.4) and DoJSON's ResponseError carries
// the parsed Retry-After so the caller can schedule its own retry.
func TestDoJSON429SurfacesRetryAfterSeconds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	e := newExecutor(t, srv, &stubAuth{})
	err := DoJSON(context.Background(), e, http.MethodGet, srv.URL, nil, nil, []int{200}, nil)
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 429 || re.RetryAfter != 17*time.Second {
		t.Fatalf("err=%#v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1", calls.Load())
	}
}

func TestDoJSON429SurfacesRetryAfterDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", time.Now().Add(90*time.Second).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	err := DoJSON(context.Background(), newExecutor(t, srv, &stubAuth{}), http.MethodGet, srv.URL, nil, nil, []int{200}, nil)
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.RetryAfter < 80*time.Second || re.RetryAfter > 91*time.Second {
		t.Fatalf("err=%#v", err)
	}
}

func TestDoJSONNoRetryAfterHeaderIsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	err := DoJSON(context.Background(), newExecutor(t, srv, &stubAuth{}), http.MethodGet, srv.URL, nil, nil, []int{200}, nil)
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.RetryAfter != 0 {
		t.Fatalf("err=%#v", err)
	}
}

// An exhausted 503 with Retry-After: the CommunicationError carries it.
func TestExecutor503ExhaustionCarriesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := newExecutor(t, srv, &stubAuth{}).Do(context.Background(), &Request{Method: http.MethodGet, URL: srv.URL})
	var ce *cxerrors.CommunicationError
	if !errors.As(err, &ce) || ce.StatusCode != 503 || ce.RetryAfter != 5*time.Second {
		t.Fatalf("err=%#v", err)
	}
}
