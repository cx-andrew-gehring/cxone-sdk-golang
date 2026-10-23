package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/observability"
)

// retryMetrics records IncRetry reasons.
type retryMetrics struct {
	observability.NopMetrics
	mu      sync.Mutex
	reasons []string
}

func (m *retryMetrics) IncRetry(_ context.Context, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reasons = append(m.reasons, reason)
}

func (m *retryMetrics) got() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.reasons...)
}

// rateLimitServer answers 429 (with retryAfter when non-empty) for the
// first n429 calls, then 200. It records each request body.
type rateLimitServer struct {
	calls  atomic.Int32
	mu     sync.Mutex
	bodies []string
}

func (s *rateLimitServer) start(t *testing.T, n429 int32, retryAfter string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(b))
		s.mu.Unlock()
		if s.calls.Add(1) <= n429 {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func rateLimitExecutor(srv *httptest.Server, maxWait time.Duration, m observability.Metrics, l observability.Logger) *Executor {
	return NewExecutor(Config{
		HTTPClient:       srv.Client(),
		Authenticator:    &stubAuth{},
		CorrelationID:    "corr-429",
		RetryPolicy:      fastRetry(),
		RateLimitMaxWait: maxWait,
		Metrics:          m,
		Logger:           l,
	})
}

// Off (the default): a 429 is returned at once, one HTTP call, no retry
// metric, and DoJSON's ResponseError carries Retry-After. Same as before the
// opt-in existed.
func TestRateLimitOptInOffReturns429Immediately(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 100, "7")
	m := &retryMetrics{}
	e := rateLimitExecutor(srv, 0, m, nil)
	err := DoJSON(context.Background(), e, http.MethodGet, srv.URL, nil, nil, []int{200}, nil)
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 429 || re.RetryAfter != 7*time.Second {
		t.Fatalf("err=%#v", err)
	}
	if s.calls.Load() != 1 || len(m.got()) != 0 {
		t.Fatalf("calls=%d retries=%v, want 1 call and no retry", s.calls.Load(), m.got())
	}
}

// On, no Retry-After: retried with the policy backoff, retry hooks fire.
func TestRateLimitOptInRetriesWithoutRetryAfter(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 1, "")
	m := &retryMetrics{}
	l := &captureLogger{}
	e := rateLimitExecutor(srv, time.Minute, m, l)
	resp, err := e.Do(context.Background(), &Request{Method: http.MethodGet, URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || s.calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d", resp.StatusCode, s.calls.Load())
	}
	if got := m.got(); len(got) != 1 || got[0] != "status_429" {
		t.Fatalf("IncRetry reasons=%v, want [status_429]", got)
	}
	if !l.Saw(observability.EventRequestRetry) {
		t.Fatalf("EventRequestRetry not logged: %v", l.events())
	}
}

// On: the wait honours Retry-After (seconds) even though the policy delay is 0.
func TestRateLimitOptInHonoursRetryAfter(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 1, "1")
	e := rateLimitExecutor(srv, time.Minute, nil, nil)
	start := time.Now()
	resp, err := e.Do(context.Background(), &Request{Method: http.MethodGet, URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if el := time.Since(start); el < 900*time.Millisecond {
		t.Fatalf("elapsed %s, want >= ~1s (Retry-After)", el)
	}
	if s.calls.Load() != 2 {
		t.Fatalf("calls=%d", s.calls.Load())
	}
}

// On: Retry-After as an HTTP-date is honoured too.
func TestRateLimitOptInHonoursRetryAfterDate(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 1, time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))
	e := rateLimitExecutor(srv, time.Minute, nil, nil)
	start := time.Now()
	resp, err := e.Do(context.Background(), &Request{Method: http.MethodGet, URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if el := time.Since(start); el < 500*time.Millisecond {
		t.Fatalf("elapsed %s, want a wait from the HTTP-date", el)
	}
}

// On: Retry-After above maxWait is not waited for; the 429 is returned.
func TestRateLimitOptInRetryAfterAboveMaxWaitReturns429(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 100, "120")
	e := rateLimitExecutor(srv, time.Minute, nil, nil)
	err := DoJSON(context.Background(), e, http.MethodGet, srv.URL, nil, nil, []int{200}, nil)
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 429 || re.RetryAfter != 120*time.Second {
		t.Fatalf("err=%#v", err)
	}
	if s.calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1", s.calls.Load())
	}
}

// On: retries stay inside the attempt budget; the last 429 is returned as
// the ResponseError (with RetryAfter), as when the opt-in is off.
func TestRateLimitOptInBudgetExhaustionReturns429(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 100, "0")
	m := &retryMetrics{}
	e := rateLimitExecutor(srv, time.Minute, m, nil)
	err := DoJSON(context.Background(), e, http.MethodGet, srv.URL, nil, nil, []int{200}, nil)
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 429 {
		t.Fatalf("err=%#v", err)
	}
	if s.calls.Load() != 3 || len(m.got()) != 2 {
		t.Fatalf("calls=%d retries=%v, want 3 calls, 2 retries", s.calls.Load(), m.got())
	}
}

// On: a POST is retried too (a 429 means the server did not process it) and
// the body is re-sent byte-for-byte.
func TestRateLimitOptInRetriesPOSTWithBody(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 1, "")
	e := rateLimitExecutor(srv, time.Minute, nil, nil)
	resp, err := e.Do(context.Background(), &Request{
		Method: http.MethodPost, URL: srv.URL, ContentType: "application/json",
		Body: strings.NewReader(`{"a":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) != 2 || s.bodies[0] != `{"a":1}` || s.bodies[1] != `{"a":1}` {
		t.Fatalf("bodies=%q", s.bodies)
	}
}

// On: a Retry-After that would outlast the caller's deadline is not waited
// for; the 429 comes back (not a deadline error).
func TestRateLimitOptInRetryAfterBeyondDeadlineReturns429(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 100, "30")
	e := rateLimitExecutor(srv, time.Minute, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := DoJSON(ctx, e, http.MethodGet, srv.URL, nil, nil, []int{200}, nil)
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 429 {
		t.Fatalf("err=%#v", err)
	}
	if s.calls.Load() != 1 {
		t.Fatalf("calls=%d", s.calls.Load())
	}
}

// On: cancellation during the Retry-After wait propagates promptly.
func TestRateLimitOptInWaitIsContextAware(t *testing.T) {
	s := &rateLimitServer{}
	srv := s.start(t, 100, "30")
	e := rateLimitExecutor(srv, time.Minute, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := e.Do(ctx, &Request{Method: http.MethodGet, URL: srv.URL})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if time.Since(start) > 5*time.Second || s.calls.Load() != 1 {
		t.Fatalf("elapsed=%s calls=%d", time.Since(start), s.calls.Load())
	}
}
