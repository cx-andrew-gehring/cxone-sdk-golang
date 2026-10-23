package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/auth"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/redact"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/observability"
)

// Config configures an [Executor]. The cxone client builder constructs one
// from the user-supplied options and stashes the resulting executor on the
// client.
type Config struct {
	HTTPClient    *http.Client
	Authenticator auth.Authenticator
	UserAgent     string
	CorrelationID string
	Timeout       time.Duration
	RetryPolicy   retry.Policy
	Interceptors  []Interceptor
	Logger        observability.Logger
	Metrics       observability.Metrics
	Tracer        observability.Tracer

	// RateLimitMaxWait opts in to retrying HTTP 429 inside RetryPolicy's
	// attempt budget (see Executor.Do). 0 (the default) keeps CLAUDE.md §4.4:
	// a 429 is returned to the caller at once.
	RateLimitMaxWait time.Duration
}

// Executor is the SDK's single request funnel. CLAUDE.md §4.3 — every HTTP
// call passes through Executor.Do, which applies headers, auth, retry,
// observability, and the interceptor pipeline.
//
// Executor is safe for concurrent use; create one per cxone.Client and share
// it for the lifetime of the client.
type Executor struct {
	cfg Config
}

// NewExecutor returns an Executor that wraps cfg. The cxone client builder
// is the only expected caller; tests in this package construct one directly.
func NewExecutor(cfg Config) *Executor {
	if cfg.Logger == nil {
		cfg.Logger = observability.NopLogger{}
	}
	if cfg.Metrics == nil {
		cfg.Metrics = observability.NopMetrics{}
	}
	if cfg.Tracer == nil {
		cfg.Tracer = observability.NopTracer{}
	}
	if cfg.RetryPolicy.MaxAttempts == 0 {
		cfg.RetryPolicy = retry.DefaultPolicy()
	}
	return &Executor{cfg: cfg}
}

// Authenticator returns the token source the funnel uses. It exists for the
// one documented non-funnel call, the presigned upload PUT, which must send
// the same (lazily fetched, refresh-on-401) bearer token when the storage URL
// is on the API origin.
func (e *Executor) Authenticator() auth.Authenticator { return e.cfg.Authenticator }

// Do executes req per CLAUDE.md §4.3:
//
//  1. Apply per-call timeout (req.Timeout overrides the executor default).
//  2. Open a tracing span "cxone.request".
//  3. For each attempt up to RetryPolicy.MaxAttempts:
//     a. Ensure an auth token is available (lazy first call).
//     b. Merge the mandatory headers on top of req.Header.
//     c. Run the Before interceptors.
//     d. Send the HTTP request.
//     e. On 401, refresh the token and retry once OFF the retry budget.
//     f. On 5xx or transient transport error, log + metric + maybe retry.
//     f2. Only with Config.RateLimitMaxWait > 0 (opt-in): 429, same way.
//     g. On success or non-retryable status, run After interceptors and
//        return the response to the caller.
//
// On exhaustion the returned error is a *cxerrors.CommunicationError. On
// auth-flow failures it is a *cxerrors.AuthError. On context cancellation
// the cancellation error is propagated immediately.
//
// Body lifecycle: Do reads req.Body once and buffers it in memory if it is
// not natively re-seekable. The returned Response.Body MUST be closed by
// the caller.
func (e *Executor) Do(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, &cxerrors.ConfigurationError{Field: "request", Reason: "request is nil"}
	}
	if req.Method == "" {
		return nil, &cxerrors.ConfigurationError{Field: "request.Method", Reason: "is required"}
	}
	if req.URL == "" {
		return nil, &cxerrors.ConfigurationError{Field: "request.URL", Reason: "is required"}
	}

	// Apply per-call timeout (overrides client default).
	//
	// We deliberately do NOT defer cancel() here. The derived context is
	// attached to the underlying *http.Request via http.NewRequestWithContext
	// in sendOnce, so cancelling it would also cancel any in-flight reads of
	// the response body — and the caller hasn't read the body yet by the
	// time Do returns. On the success path we attach cancel to the returned
	// Response.Body so it fires when the caller closes the body; on every
	// error path we call cancel explicitly before returning so the timer
	// goroutine is released.
	timeout := req.Timeout
	if timeout == 0 {
		timeout = e.cfg.Timeout
	}
	var timeoutCancel context.CancelFunc
	if timeout > 0 {
		ctx, timeoutCancel = context.WithTimeout(ctx, timeout)
	}
	cancelTimeout := func() {
		if timeoutCancel != nil {
			timeoutCancel()
			timeoutCancel = nil
		}
	}

	// Open span for the whole funnel call.
	ctx, span := e.cfg.Tracer.Start(ctx, "cxone.request")
	defer span.End()
	sanitizedURL := redact.URL(req.URL)
	span.SetAttribute("http.method", req.Method)
	span.SetAttribute("http.url", sanitizedURL)
	span.SetAttribute("cxone.correlation_id", e.cfg.CorrelationID)

	// Buffer the body once so retries can re-send it.
	bodyBytes, err := readAll(req.Body)
	if err != nil {
		cancelTimeout()
		return nil, &cxerrors.CommunicationError{
			Method: req.Method, URL: sanitizedURL, Attempt: 1,
			CorrelationID: e.cfg.CorrelationID,
			Cause:         fmt.Errorf("read request body: %w", err),
		}
	}

	var (
		resp           *Response
		lastErr        error
		usedGen        uint64
		refreshedAfter401 bool
	)

	final, err := retry.Do(ctx, e.cfg.RetryPolicy, func(attempt int) (retry.Outcome, error) {
		span.SetAttribute("cxone.attempt", attempt)

		// Ensure a token (lazy first call). On retry attempts after a 401
		// refresh, usedGen is already updated below — we just re-read the
		// cached token here.
		token, gen, authErr := e.cfg.Authenticator.Token(ctx)
		if authErr != nil {
			lastErr = authErr
			// Auth fetch failed — surface immediately, do not retry.
			return retry.OutcomeFatal, authErr
		}
		usedGen = gen

		httpResp, sendErr := e.sendOnce(ctx, req, bodyBytes, token, attempt)

		// Transport-level error.
		if sendErr != nil {
			if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
				lastErr = sendErr
				return retry.OutcomeFatal, sendErr
			}
			e.cfg.Metrics.IncRequest(ctx, req.Method, 0)
			e.runAfter(ctx, req, nil, sendErr)
			if retry.IsTransientNetworkError(sendErr) {
				e.logRetry(ctx, req, sanitizedURL, attempt, 0, sendErr)
				e.cfg.Metrics.IncRetry(ctx, "transport")
				lastErr = sendErr
				return retry.OutcomeRetry, sendErr
			}
			lastErr = &cxerrors.CommunicationError{
				Method: req.Method, URL: sanitizedURL, Attempt: attempt,
				CorrelationID: e.cfg.CorrelationID, Cause: sendErr,
			}
			return retry.OutcomeFatal, lastErr
		}

		// 401 → refresh + one off-budget retry.
		if httpResp.StatusCode == http.StatusUnauthorized && !refreshedAfter401 {
			drain(httpResp.Body)
			refreshedAfter401 = true
			e.cfg.Logger.Info(ctx, observability.EventAuthRefresh,
				observability.F("reason", "401"),
			)
			e.cfg.Metrics.IncAuthRefresh(ctx, "401")
			newToken, _, refreshErr := e.cfg.Authenticator.Refresh(ctx, usedGen)
			if refreshErr != nil {
				lastErr = refreshErr
				return retry.OutcomeFatal, refreshErr
			}
			// Retry once OFF the budget by issuing a second send inline.
			// retry.Do increments attempt on every iteration, so we cannot
			// piggy-back on it for the off-budget retry; we send once here
			// and fall through to the regular success/failure handling.
			httpResp2, sendErr2 := e.sendOnce(ctx, req, bodyBytes, newToken, attempt)
			if sendErr2 != nil {
				if retry.IsTransientNetworkError(sendErr2) {
					lastErr = sendErr2
					return retry.OutcomeRetry, sendErr2
				}
				lastErr = &cxerrors.CommunicationError{
					Method: req.Method, URL: sanitizedURL, Attempt: attempt,
					CorrelationID: e.cfg.CorrelationID, Cause: sendErr2,
				}
				return retry.OutcomeFatal, lastErr
			}
			httpResp = httpResp2
		}

		span.SetAttribute("http.status_code", httpResp.StatusCode)
		e.cfg.Metrics.IncRequest(ctx, req.Method, httpResp.StatusCode)

		if wait, ok := e.rateLimitRetry(ctx, httpResp, attempt); ok {
			drain(httpResp.Body)
			e.logRetry(ctx, req, sanitizedURL, attempt, httpResp.StatusCode, nil)
			e.cfg.Metrics.IncRetry(ctx, "status_429")
			lastErr = &cxerrors.CommunicationError{
				Method: req.Method, URL: sanitizedURL, StatusCode: httpResp.StatusCode,
				Attempt: attempt, CorrelationID: e.cfg.CorrelationID,
				RetryAfter: wait,
			}
			return retry.OutcomeRetry, retry.WithDelay(lastErr, wait)
		}

		switch retry.ClassifyStatus(httpResp.StatusCode) {
		case retry.OutcomeSuccess:
			e.runAfter(ctx, req, httpResp, nil)
			resp = httpResp
			return retry.OutcomeSuccess, nil

		case retry.OutcomeRetry:
			drain(httpResp.Body)
			e.logRetry(ctx, req, sanitizedURL, attempt, httpResp.StatusCode, nil)
			e.cfg.Metrics.IncRetry(ctx, fmt.Sprintf("status_%d", httpResp.StatusCode))
			lastErr = &cxerrors.CommunicationError{
				Method: req.Method, URL: sanitizedURL, StatusCode: httpResp.StatusCode,
				Attempt: attempt, CorrelationID: e.cfg.CorrelationID,
				RetryAfter: retryAfter(httpResp.Header),
			}
			return retry.OutcomeRetry, lastErr

		default: // OutcomeFatal — non-retryable, non-success status.
			e.runAfter(ctx, req, httpResp, nil)
			resp = httpResp
			return retry.OutcomeSuccess, nil
		}
	})

	if err != nil {
		cancelTimeout()
		span.SetStatus(err)
		// retry.Do returned the last retry-able error verbatim. Wrap as
		// CommunicationError so callers always see the catalog type unless
		// the error is already from the catalog (e.g. AuthError).
		var (
			commErr *cxerrors.CommunicationError
			authErr *cxerrors.AuthError
		)
		if errors.As(err, &commErr) || errors.As(err, &authErr) {
			return nil, err
		}
		return nil, &cxerrors.CommunicationError{
			Method:        req.Method,
			URL:           sanitizedURL,
			Attempt:       final,
			CorrelationID: e.cfg.CorrelationID,
			Cause:         err,
		}
	}

	// Attach the per-call cancel to Response.Body.Close so the request
	// context stays alive while the caller reads the body, but the timer
	// goroutine is still released when the caller is done.
	if timeoutCancel != nil && resp != nil && resp.Body != nil {
		resp.Body = &bodyWithCancel{ReadCloser: resp.Body, cancel: cancelTimeout}
	} else {
		cancelTimeout()
	}
	return resp, nil
}

// rateLimitRetry decides whether a response is a 429 the opt-in retries
// (Config.RateLimitMaxWait > 0). It returns the server's Retry-After (0 when
// absent: the policy backoff applies) and true when:
//   - the opt-in is on and the status is 429,
//   - attempt is not the last one (the last 429 is returned to the caller
//     unchanged, as with the opt-in off),
//   - Retry-After is at most RateLimitMaxWait, and
//   - Retry-After ends before ctx's deadline (waiting past it would turn a
//     429 the caller can act on into a deadline error).
//
// Any method is retried: a 429 means the server rejected the request without
// processing it, so re-sending a POST cannot duplicate its effect.
func (e *Executor) rateLimitRetry(ctx context.Context, resp *Response, attempt int) (time.Duration, bool) {
	if e.cfg.RateLimitMaxWait <= 0 || resp.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	if attempt >= e.cfg.RetryPolicy.MaxAttempts {
		return 0, false
	}
	wait := retryAfter(resp.Header)
	if wait > e.cfg.RateLimitMaxWait {
		return 0, false
	}
	if dl, ok := ctx.Deadline(); ok && wait > 0 && time.Until(dl) <= wait {
		return 0, false
	}
	return wait, true
}

// bodyWithCancel wraps a response body so closing it also releases the
// per-call timeout context's cancel function. Close is safe to call more
// than once — the embedded cancelTimeout closure no-ops after the first
// invocation.
type bodyWithCancel struct {
	io.ReadCloser
	cancel func()
}

func (b *bodyWithCancel) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
	}
	return err
}

// sendOnce builds and dispatches a single HTTP request. It runs the Before
// interceptors and the http.Client call but does NOT classify the result —
// the caller (Do) decides whether to retry, refresh, or return.
func (e *Executor) sendOnce(ctx context.Context, req *Request, bodyBytes []byte, token string, attempt int) (*Response, error) {
	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}
	// User-supplied headers first, then mandatory headers (which take
	// precedence on collision).
	for k, vs := range req.Header {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	if req.ContentType != "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", HeaderAccept)
	if e.cfg.UserAgent != "" {
		httpReq.Header.Set("User-Agent", e.cfg.UserAgent)
	}
	if e.cfg.CorrelationID != "" {
		httpReq.Header.Set(HeaderCorrelationID, e.cfg.CorrelationID)
	}

	// Wrap the request in our internal Request shape for interceptors.
	icReq := &Request{
		Method:      req.Method,
		URL:         req.URL,
		Header:      httpReq.Header,
		ContentType: req.ContentType,
		Timeout:     req.Timeout,
	}
	for _, ic := range e.cfg.Interceptors {
		if err := ic.Before(ctx, icReq, attempt); err != nil {
			return nil, err
		}
	}

	e.cfg.Logger.Info(ctx, observability.EventRequestStart,
		observability.F("method", req.Method),
		observability.F("url", redact.URL(req.URL)),
		observability.F("attempt", attempt),
		observability.F("correlationId", e.cfg.CorrelationID),
	)

	start := time.Now()
	httpResp, err := e.cfg.HTTPClient.Do(httpReq)
	durMs := float64(time.Since(start).Microseconds()) / 1000.0
	e.cfg.Metrics.ObserveRequestLatency(ctx, req.Method, durMs)

	if err != nil {
		e.cfg.Logger.Warn(ctx, observability.EventRequestEnd,
			observability.F("method", req.Method),
			observability.F("url", redact.URL(req.URL)),
			observability.F("durationMs", durMs),
			observability.F("error", redact.String(err.Error())),
			observability.F("attempt", attempt),
			observability.F("correlationId", e.cfg.CorrelationID),
		)
		return nil, err
	}

	e.cfg.Logger.Info(ctx, observability.EventRequestEnd,
		observability.F("method", req.Method),
		observability.F("url", redact.URL(req.URL)),
		observability.F("status", httpResp.StatusCode),
		observability.F("durationMs", durMs),
		observability.F("attempt", attempt),
		observability.F("correlationId", e.cfg.CorrelationID),
	)

	return &Response{
		StatusCode:    httpResp.StatusCode,
		Header:        httpResp.Header,
		Body:          httpResp.Body,
		Attempt:       attempt,
		CorrelationID: e.cfg.CorrelationID,
	}, nil
}

// runAfter invokes the After interceptors in reverse registration order.
// Errors from After replace the original error.
func (e *Executor) runAfter(ctx context.Context, req *Request, resp *Response, sendErr error) {
	if len(e.cfg.Interceptors) == 0 {
		return
	}
	icReq := &Request{Method: req.Method, URL: req.URL, Header: req.Header}
	for i := len(e.cfg.Interceptors) - 1; i >= 0; i-- {
		_ = e.cfg.Interceptors[i].After(ctx, icReq, resp, sendErr)
	}
}

func (e *Executor) logRetry(ctx context.Context, req *Request, sanitizedURL string, attempt, status int, sendErr error) {
	fields := []observability.Field{
		observability.F("method", req.Method),
		observability.F("url", sanitizedURL),
		observability.F("attempt", attempt),
		observability.F("correlationId", e.cfg.CorrelationID),
	}
	if status != 0 {
		fields = append(fields, observability.F("status", status))
	}
	if sendErr != nil {
		fields = append(fields, observability.F("error", redact.String(sendErr.Error())))
	}
	e.cfg.Logger.Warn(ctx, observability.EventRequestRetry, fields...)
}

// readAll buffers an io.Reader into a byte slice. nil and empty bodies are
// preserved. Closes the body if it implements io.Closer.
func readAll(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r)
	if c, ok := r.(io.Closer); ok {
		_ = c.Close()
	}
	return b, err
}

// drain consumes and closes a response body so the connection can be
// returned to the pool.
func drain(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, 64*1024))
	_ = rc.Close()
}


