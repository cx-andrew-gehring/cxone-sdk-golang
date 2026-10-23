package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
)

// fastRetry is a zero-delay 3-attempt policy used by every test in this file
// so retry sleeps don't dominate the test runtime.
var fastRetry = retry.Policy{MaxAttempts: 3, MaxDelay: 0}

// newTestServer wraps a handler so each test can set its own response logic
// and observe the requests made.
func newTestServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, Config) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, Config{
		HTTPClient:  srv.Client(),
		TokenURL:    srv.URL + "/auth/realms/acme/protocol/openid-connect/token",
		UserAgent:   "test/(CxOne GoSDK/0.0.0-test)",
		RetryPolicy: fastRetry,
	}
}

func TestOAuthSuccess(t *testing.T) {
	var (
		gotForm string
		gotUA   string
	)
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm = string(body)
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"abc.def.ghi","token_type":"Bearer","expires_in":300}`)
	})
	a, err := NewOAuthClientCredentials(cfg, OAuthCredentials{ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	tok, gen, err := a.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "abc.def.ghi" || gen != 1 {
		t.Errorf("got token=%q gen=%d", tok, gen)
	}
	if !strings.Contains(gotForm, "grant_type=client_credentials") ||
		!strings.Contains(gotForm, "client_id=id") ||
		!strings.Contains(gotForm, "client_secret=secret") {
		t.Errorf("unexpected form: %q", gotForm)
	}
	if gotUA == "" {
		t.Errorf("User-Agent not sent")
	}
}

func TestAPIKeySuccess(t *testing.T) {
	var gotForm string
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok-1"}`)
	})
	a, err := NewAPIKey(cfg, "my-refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := a.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok-1" {
		t.Errorf("token=%q", tok)
	}
	for _, want := range []string{"grant_type=refresh_token", "client_id=ast-app", "refresh_token=my-refresh-token"} {
		if !strings.Contains(gotForm, want) {
			t.Errorf("missing %q in form: %q", want, gotForm)
		}
	}
}

func TestTokenCachedAcrossCalls(t *testing.T) {
	var hits int32
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"tok-%d"}`, atomic.LoadInt32(&hits))
	})
	a, _ := NewAPIKey(cfg, "rt")
	for i := 0; i < 5; i++ {
		if _, _, err := a.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 IAM call, got %d", got)
	}
}

func TestRefreshSerializedAcrossGoroutines(t *testing.T) {
	var hits int32
	releaseCh := make(chan struct{})
	releaseOnce := sync.Once{}
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		// Block ONLY the very first request long enough that all goroutines
		// pile up on the cache mutex. Subsequent requests (none expected) go
		// through immediately so the test can fail fast.
		if n == 1 {
			<-releaseCh
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"tok-%d"}`, n)
	})
	a, _ := NewAPIKey(cfg, "rt")

	// Pre-seed gen 1 with a synchronous Token call.
	releaseOnce.Do(func() { close(releaseCh) })
	tok, gen, err := a.Token(context.Background())
	if err != nil || tok != "tok-1" || gen != 1 {
		t.Fatalf("seed failed: tok=%q gen=%d err=%v", tok, gen, err)
	}

	// Now have N goroutines all call Refresh(staleGen=1) concurrently.
	// Exactly ONE additional IAM hit must occur (gen → 2); all other
	// goroutines should see the cached gen-2 token.
	const N = 32
	var wg sync.WaitGroup
	tokens := make([]string, N)
	gens := make([]uint64, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			tok, gen, err := a.Refresh(context.Background(), 1)
			if err != nil {
				t.Errorf("refresh err: %v", err)
				return
			}
			tokens[i] = tok
			gens[i] = gen
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 total IAM calls (seed + 1 refresh), got %d", got)
	}
	for i := 0; i < N; i++ {
		if tokens[i] != "tok-2" || gens[i] != 2 {
			t.Errorf("goroutine %d: tok=%q gen=%d, want tok-2/2", i, tokens[i], gens[i])
		}
	}
}

func TestRefreshNoOpWhenAdvanced(t *testing.T) {
	var hits int32
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"tok-%d"}`, n)
	})
	a, _ := NewAPIKey(cfg, "rt")

	if _, _, err := a.Token(context.Background()); err != nil { // gen=1
		t.Fatal(err)
	}
	if _, _, err := a.Refresh(context.Background(), 1); err != nil { // gen=2
		t.Fatal(err)
	}
	// staleGen=1 but cache is at gen=2 → should be a no-op.
	tok, gen, err := a.Refresh(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok-2" || gen != 2 {
		t.Errorf("got tok=%q gen=%d, want tok-2/2", tok, gen)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 IAM calls (no extra fetch), got %d", got)
	}
}

func TestAuthRetriesOn5xx(t *testing.T) {
	var hits int32
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"ok"}`)
	})
	a, _ := NewAPIKey(cfg, "rt")
	tok, _, err := a.Token(context.Background())
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if tok != "ok" {
		t.Errorf("token=%q", tok)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestAuthFatalOn400(t *testing.T) {
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"bad creds"}`)
	})
	a, _ := NewAPIKey(cfg, "rt")
	_, _, err := a.Token(context.Background())
	var aerr *cxerrors.AuthError
	if !errors.As(err, &aerr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if aerr.StatusCode != 401 {
		t.Errorf("status=%d", aerr.StatusCode)
	}
	if !strings.Contains(aerr.Reason, "invalid_grant") {
		t.Errorf("reason=%q", aerr.Reason)
	}
}

func TestAuthErrorDoesNotLeakSecret(t *testing.T) {
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_request"}`)
	})
	a, _ := NewOAuthClientCredentials(cfg, OAuthCredentials{ClientID: "the-id", ClientSecret: "TOPSECRET-VALUE"})
	_, _, err := a.Token(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "TOPSECRET-VALUE") {
		t.Fatalf("client_secret leaked: %q", err.Error())
	}
}

func TestAuthRespectsContextCancellation(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	_, cfg := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	a, _ := NewAPIKey(cfg, "rt")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Allow the request to start, then cancel.
		cancel()
	}()
	_, _, err := a.Token(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		// httptest may surface the cancellation as a wrapped url.Error;
		// accept either by checking for the canonical sentinel.
		if !strings.Contains(strings.ToLower(fmt.Sprint(err)), "canceled") {
			t.Errorf("expected cancellation, got %v", err)
		}
	}
}

func TestNewOAuthValidation(t *testing.T) {
	_, cfg := newTestServer(t, func(http.ResponseWriter, *http.Request) {})
	cases := []struct {
		name  string
		cfg   Config
		creds OAuthCredentials
	}{
		{"missing client id", cfg, OAuthCredentials{ClientSecret: "x"}},
		{"missing client secret", cfg, OAuthCredentials{ClientID: "x"}},
		{"missing http client", Config{TokenURL: cfg.TokenURL}, OAuthCredentials{ClientID: "a", ClientSecret: "b"}},
		{"missing token URL", Config{HTTPClient: cfg.HTTPClient}, OAuthCredentials{ClientID: "a", ClientSecret: "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOAuthClientCredentials(tc.cfg, tc.creds)
			var ce *cxerrors.ConfigurationError
			if !errors.As(err, &ce) {
				t.Fatalf("expected *ConfigurationError, got %T: %v", err, err)
			}
		})
	}
}
