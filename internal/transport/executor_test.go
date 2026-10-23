package transport

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
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/auth"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/observability"
)

// fastRetry returns a 3-attempt zero-delay policy.
func fastRetry() retry.Policy { return retry.Policy{MaxAttempts: 3, MaxDelay: 0} }

// stubAuth is a fake Authenticator for funnel tests. It hands out tokens
// labeled "tok-N" where N is the generation, increments on each Refresh,
// and counts calls so tests can assert serialization.
type stubAuth struct {
	mu    sync.Mutex
	gen   uint64
	calls int
	err   error
}

func (s *stubAuth) Token(ctx context.Context) (string, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", 0, s.err
	}
	if s.gen == 0 {
		s.gen = 1
		s.calls++
	}
	return fmt.Sprintf("tok-%d", s.gen), s.gen, nil
}

func (s *stubAuth) Refresh(ctx context.Context, staleGen uint64) (string, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", 0, s.err
	}
	if s.gen > staleGen {
		return fmt.Sprintf("tok-%d", s.gen), s.gen, nil
	}
	s.gen++
	s.calls++
	return fmt.Sprintf("tok-%d", s.gen), s.gen, nil
}

func newExecutor(t *testing.T, srv *httptest.Server, a auth.Authenticator, ics ...Interceptor) *Executor {
	t.Helper()
	return NewExecutor(Config{
		HTTPClient:    srv.Client(),
		Authenticator: a,
		UserAgent:     "test/(CxOne GoSDK/0.0.0-test)",
		CorrelationID: "corr-1234",
		RetryPolicy:   fastRetry(),
		Interceptors:  ics,
	})
}

func TestExecutorSendsMandatoryHeaders(t *testing.T) {
	var (
		gotAccept    string
		gotAuth      string
		gotUA        string
		gotCorrID    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotCorrID = r.Header.Get("CorrelationId")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := newExecutor(t, srv, &stubAuth{})
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/projects"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if gotAccept != HeaderAccept {
		t.Errorf("Accept = %q, want %q", gotAccept, HeaderAccept)
	}
	if gotAuth != "Bearer tok-1" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if !strings.HasPrefix(gotUA, "test/(CxOne GoSDK/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotCorrID != "corr-1234" {
		t.Errorf("CorrelationId = %q", gotCorrID)
	}
}

func TestExecutor401TriggersRefreshAndRetry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Verify the second hit carries the new token.
		if got := r.Header.Get("Authorization"); got != "Bearer tok-2" {
			t.Errorf("retry sent with wrong token: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &stubAuth{}
	exec := newExecutor(t, srv, a)
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 HTTP hits, got %d", got)
	}
}

func TestExecutorRetriesOn5xxThenSucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := newExecutor(t, srv, &stubAuth{})
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestExecutor5xxExhaustionReturnsCommunicationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	exec := newExecutor(t, srv, &stubAuth{})
	_, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	var commErr *cxerrors.CommunicationError
	if !errors.As(err, &commErr) {
		t.Fatalf("expected *CommunicationError, got %T: %v", err, err)
	}
	if commErr.StatusCode != 502 {
		t.Errorf("status = %d", commErr.StatusCode)
	}
	if commErr.Attempt != 3 {
		t.Errorf("attempt = %d", commErr.Attempt)
	}
	if commErr.CorrelationID != "corr-1234" {
		t.Errorf("correlationID = %q", commErr.CorrelationID)
	}
}

func TestExecutor4xxFatalNoRetry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"bad"}`)
	}))
	defer srv.Close()

	exec := newExecutor(t, srv, &stubAuth{})
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err != nil {
		t.Fatalf("400 should not return error from executor (caller decodes body): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 hit, got %d", got)
	}
}

func TestExecutorContextCancellationPropagates(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	exec := newExecutor(t, srv, &stubAuth{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := exec.Do(ctx, &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(strings.ToLower(err.Error()), "cancel") {
		t.Errorf("expected cancellation, got %v", err)
	}
}

func TestExecutorPerCallTimeoutOverridesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	exec := NewExecutor(Config{
		HTTPClient:    srv.Client(),
		Authenticator: &stubAuth{},
		Timeout:       time.Hour, // would normally hang the test
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	start := time.Now()
	_, err := exec.Do(context.Background(), &Request{
		Method:  "GET",
		URL:     srv.URL + "/api/x",
		Timeout: 30 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("per-call timeout did not fire: %v", elapsed)
	}
}

func TestExecutorErrorDoesNotLeakBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	exec := newExecutor(t, srv, &stubAuth{})
	_, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "tok-1") || strings.Contains(err.Error(), "Bearer ") {
		// Bearer + REDACTED is fine; literal tokens are not.
		if !strings.Contains(err.Error(), "REDACTED") {
			t.Errorf("error contains a non-redacted bearer token: %q", err.Error())
		}
	}
}

func TestExecutorTransientTransportErrorRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			// Hijack and close the connection without writing a response.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("hijacker required")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := newExecutor(t, srv, &stubAuth{})
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err != nil {
		t.Fatalf("expected success after transport retries, got %v", err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&hits); got < 2 {
		t.Errorf("expected at least 2 attempts, got %d", got)
	}
}

func TestInterceptorBeforeAndAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "added" {
			t.Errorf("interceptor header missing")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var afterCalled bool
	ic := InterceptorFunc{
		BeforeFn: func(ctx context.Context, req *Request, attempt int) error {
			req.Header.Set("X-Test", "added")
			return nil
		},
		AfterFn: func(ctx context.Context, req *Request, resp *Response, err error) error {
			afterCalled = true
			return err
		},
	}
	exec := newExecutor(t, srv, &stubAuth{}, ic)
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !afterCalled {
		t.Errorf("After interceptor not called")
	}
}

func TestInterceptorBeforeErrorAborts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be reached")
	}))
	defer srv.Close()

	want := errors.New("denied")
	ic := InterceptorFunc{
		BeforeFn: func(ctx context.Context, req *Request, attempt int) error { return want },
	}
	exec := newExecutor(t, srv, &stubAuth{}, ic)
	_, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err == nil || !errors.Is(err, want) {
		// The funnel may wrap as CommunicationError; accept either as long as
		// the underlying cause is the original sentinel.
		var commErr *cxerrors.CommunicationError
		if errors.As(err, &commErr) && errors.Is(commErr.Cause, want) {
			return
		}
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestExecutorRequestStartIsLogged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := &captureLogger{}
	exec := NewExecutor(Config{
		HTTPClient:    srv.Client(),
		Authenticator: &stubAuth{},
		CorrelationID: "corr",
		RetryPolicy:   fastRetry(),
		Logger:        logger,
	})
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !logger.Saw(observability.EventRequestStart) {
		t.Errorf("EventRequestStart not logged: %v", logger.events())
	}
	if !logger.Saw(observability.EventRequestEnd) {
		t.Errorf("EventRequestEnd not logged: %v", logger.events())
	}
}

// captureLogger records every event for assertions.
type captureLogger struct {
	mu     sync.Mutex
	calls  []string
}

func (c *captureLogger) record(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, msg)
}

func (c *captureLogger) Saw(msg string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.calls {
		if m == msg {
			return true
		}
	}
	return false
}

func (c *captureLogger) events() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.calls))
	copy(out, c.calls)
	return out
}

func (c *captureLogger) Debug(_ context.Context, msg string, _ ...observability.Field) { c.record(msg) }
func (c *captureLogger) Info(_ context.Context, msg string, _ ...observability.Field)  { c.record(msg) }
func (c *captureLogger) Warn(_ context.Context, msg string, _ ...observability.Field)  { c.record(msg) }
func (c *captureLogger) Error(_ context.Context, msg string, _ ...observability.Field) { c.record(msg) }

// TestExecutorBodyReadableAfterDo guards against a regression where
// Executor.Do derived a per-call timeout context with defer cancel(). The
// returned Response.Body was bound to that context, so io.ReadAll after Do
// returned failed with "context canceled" even though headers had arrived
// cleanly. Every JSON/text endpoint hit this because the body is always
// read after Do returns.
//
// The test server flushes headers, then sleeps before flushing the body —
// this models the real-world case where the body has not yet arrived by
// the time Do() returns, so the body Read actually has to wait on the
// request context. A pre-buffered small body would mask the bug.
func TestExecutorBodyReadableAfterDo(t *testing.T) {
	const want = `{"projects":[{"id":"1"},{"id":"2"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		_, _ = io.WriteString(w, want)
	}))
	defer srv.Close()

	exec := NewExecutor(Config{
		HTTPClient:    srv.Client(),
		Authenticator: &stubAuth{},
		CorrelationID: "corr-body",
		Timeout:       60 * time.Second, // matches the SDK default
		RetryPolicy:   fastRetry(),
	})
	resp, err := exec.Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/api/x"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body after Do returned: %v", err)
	}
	if string(got) != want {
		t.Errorf("body = %q, want %q", string(got), want)
	}
}

// TestExecutorPerCallTimeoutCancelsSlowBody confirms the per-call timeout
// is still enforced even though Do no longer cancels eagerly on return:
// a body that takes longer than req.Timeout to read must surface the
// deadline exceeded.
func TestExecutorPerCallTimeoutCancelsSlowBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// Send a byte so headers + first chunk land, then stall past the
		// per-call timeout so the body Read trips the deadline.
		_, _ = w.Write([]byte("x"))
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	exec := NewExecutor(Config{
		HTTPClient:    srv.Client(),
		Authenticator: &stubAuth{},
		CorrelationID: "corr-slow",
		Timeout:       time.Hour,
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	resp, err := exec.Do(context.Background(), &Request{
		Method:  "GET",
		URL:     srv.URL + "/api/slow",
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	_, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		t.Fatal("expected body read to fail when per-call timeout fires mid-stream")
	}
	if !errors.Is(readErr, context.DeadlineExceeded) &&
		!strings.Contains(strings.ToLower(readErr.Error()), "deadline") {
		t.Errorf("expected deadline error, got %v", readErr)
	}
}
