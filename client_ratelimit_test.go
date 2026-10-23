package cxone_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
)

// rateLimitedClient serves /api/projects/p1 with a 429 (Retry-After: 0) for
// the first n429 calls, then the project. build adds builder options.
func rateLimitedClient(t *testing.T, n429 int32, build func(*cxone.ClientBuilder) *cxone.ClientBuilder) (*cxone.Client, *atomic.Int32, error) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/realms/acme/protocol/openid-connect/token", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"tok","token_type":"Bearer","expires_in":300}`)
	})
	mux.HandleFunc("/api/projects/p1", func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= n429 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"id":"p1","name":"acme"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	region, _ := cxone.NewCustomRegion(host, host)
	region, _ = region.WithScheme("http")
	b := cxone.NewClient().Region(region).Tenant("acme").AgentName("test-agent").
		APIKey("dummy").HTTPClient(srv.Client()).Retries(3).RetryDelaySeconds(0)
	c, err := build(b).Build()
	if err == nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, &calls, err
}

// Default (opt-in not called): one call, and the 429 surfaces as a
// ResponseError with RetryAfter populated, exactly as CLAUDE.md §4.4 says.
func TestClientDefaultDoesNotRetry429(t *testing.T) {
	c, calls, err := rateLimitedClient(t, 1, func(b *cxone.ClientBuilder) *cxone.ClientBuilder { return b })
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Projects().Get(context.Background(), "p1")
	var re *cxone.ResponseError
	if !errors.As(err, &re) || re.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("err=%v, want 429 ResponseError", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1", calls.Load())
	}
}

func TestClientRetryOnTooManyRequestsRetries(t *testing.T) {
	c, calls, err := rateLimitedClient(t, 2, func(b *cxone.ClientBuilder) *cxone.ClientBuilder {
		return b.RetryOnTooManyRequests(time.Minute)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Projects().Get(context.Background(), "p1"); err != nil {
		t.Fatalf("err=%v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d, want 3", calls.Load())
	}
}

// RetryOnTooManyRequests(0) leaves the opt-in off.
func TestClientRetryOnTooManyRequestsZeroIsOff(t *testing.T) {
	c, calls, err := rateLimitedClient(t, 1, func(b *cxone.ClientBuilder) *cxone.ClientBuilder {
		return b.RetryOnTooManyRequests(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Projects().Get(context.Background(), "p1"); err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d, want the 429 after 1 call", err, calls.Load())
	}
}

func TestClientRetryOnTooManyRequestsRejectsNegative(t *testing.T) {
	_, _, err := rateLimitedClient(t, 0, func(b *cxone.ClientBuilder) *cxone.ClientBuilder {
		return b.RetryOnTooManyRequests(-time.Second)
	})
	var ce *cxone.ConfigurationError
	if !errors.As(err, &ce) || ce.Field != "RetryOnTooManyRequests" {
		t.Fatalf("err=%v, want ConfigurationError{RetryOnTooManyRequests}", err)
	}
}
