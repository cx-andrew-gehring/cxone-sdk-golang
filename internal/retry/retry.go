// Package retry contains the retry classifier and jittered backoff used by
// both the request funnel (root cxone package) and the auth flow (auth
// package). Keeping it in its own subpackage avoids an import cycle and
// guarantees identical behavior across both call sites — auth retries follow
// the same rules as request retries (CLAUDE.md §6.3).
package retry

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Policy controls how Do retries a fallible operation.
type Policy struct {
	// MaxAttempts is the total number of attempts (including the first).
	// Defaults to 3.
	MaxAttempts int

	// MaxDelay is the upper bound on the delay between attempts. The actual
	// delay used is uniformly random in [1s, MaxDelay] when Randomize is true,
	// otherwise the fixed MaxDelay (see §4.4).
	MaxDelay time.Duration

	// Randomize controls whether the delay is jittered. CLAUDE.md §4.4
	// requires jitter; the default policy sets this to true.
	Randomize bool

	// rng is an optional override for the random source used by jitter.
	// Tests inject a deterministic source; production callers leave it nil
	// and a package-level shared source is used.
	rng *rand.Rand
}

// DefaultPolicy returns the SDK's default retry policy: 3 attempts, jittered
// backoff in [1s, 15s]. CLAUDE.md §4.1 + §4.4.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts: 3,
		MaxDelay:    15 * time.Second,
		Randomize:   true,
	}
}

// WithRNG returns a copy of p whose jitter uses the supplied random source.
// Intended for tests that need deterministic backoff.
func (p Policy) WithRNG(r *rand.Rand) Policy {
	p.rng = r
	return p
}

// nextDelay returns the delay to wait before attempt+1 (1-indexed). Returns 0
// if attempt is already the last attempt or if MaxDelay is non-positive
// (tests use the latter to skip sleeping).
//
// When Randomize is true and MaxDelay >= 1s, the delay is uniform in
// [1s, MaxDelay] (CLAUDE.md §4.4). When MaxDelay < 1s, the lower bound is
// reduced to MaxDelay so very small policies still produce a delay; this is
// useful in tests but should not be relied on in production.
func (p Policy) nextDelay(attempt int) time.Duration {
	if attempt >= p.MaxAttempts || p.MaxDelay <= 0 {
		return 0
	}
	if !p.Randomize {
		return p.MaxDelay
	}
	minDelay := time.Second
	if p.MaxDelay < minDelay {
		minDelay = p.MaxDelay
	}
	span := int64(p.MaxDelay - minDelay)
	if span <= 0 {
		return minDelay
	}
	return minDelay + time.Duration(rngFor(p).Int63n(span+1))
}

var (
	sharedRNGMu sync.Mutex
	sharedRNG   = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // jitter only
)

// rngFor returns a *rand.Rand suitable for jitter. If the policy supplied its
// own source, that one is used directly (callers in tests construct their own
// goroutine-local Rand and need not worry about contention). Otherwise the
// shared source is wrapped in a tiny lock for cross-goroutine safety.
func rngFor(p Policy) *rand.Rand {
	if p.rng != nil {
		return p.rng
	}
	// We can't return a locked-source Rand from here; the shared call site
	// in nextDelay reads from the package-level rand under the mutex.
	sharedRNGMu.Lock()
	defer sharedRNGMu.Unlock()
	return sharedRNG
}

// Outcome describes how a single attempt finished, used by Classify and Do.
type Outcome int

const (
	// OutcomeSuccess: stop and return success.
	OutcomeSuccess Outcome = iota
	// OutcomeRetry: the operation can be retried.
	OutcomeRetry
	// OutcomeFatal: the operation failed and must not be retried.
	OutcomeFatal
)

// UnknownOutcomeError is returned by Do when fn reports an Outcome value Do
// does not know and no error of its own. An unknown outcome is fatal.
type UnknownOutcomeError struct {
	Outcome Outcome
}

func (e *UnknownOutcomeError) Error() string {
	return "retry: unknown outcome " + strconv.Itoa(int(e.Outcome))
}

// WithDelay wraps err so that, when fn returns (OutcomeRetry, WithDelay(err, d))
// and d > 0, Do waits d before the next attempt instead of the policy backoff
// (used for a server-supplied Retry-After). Do never returns the wrapper: the
// error it reports is err itself. A non-positive d leaves the policy backoff.
func WithDelay(err error, d time.Duration) error {
	return &delayHint{err: err, delay: d}
}

// delayHint carries a WithDelay hint from fn to Do. It is unwrapped by Do and
// never escapes it.
type delayHint struct {
	err   error
	delay time.Duration
}

func (h *delayHint) Error() string {
	if h.err == nil {
		return "retry: delayed retry"
	}
	return h.err.Error()
}

func (h *delayHint) Unwrap() error { return h.err }

// splitHint returns err without a top-level WithDelay wrapper and the hinted
// delay (0 when there is none).
func splitHint(err error) (error, time.Duration) {
	if h, ok := err.(*delayHint); ok { //nolint:errorlint // only Do's own top-level wrapper
		return h.err, h.delay
	}
	return err, 0
}

// ClassifyStatus returns the retry verdict for an HTTP status code per
// CLAUDE.md §4.4. 401 is intentionally NOT classified as a retry here — the
// funnel handles 401 separately (refresh + one-shot retry off the budget).
func ClassifyStatus(status int) Outcome {
	if status >= 200 && status < 300 {
		return OutcomeSuccess
	}
	switch status {
	case 500, 502, 503, 504:
		return OutcomeRetry
	}
	return OutcomeFatal
}

// IsTransientNetworkError reports whether err looks like a transient transport
// failure (connection reset/refused, DNS, read/connect timeout, proxy hiccup).
//
// CLAUDE.md §4.4 requires retrying these. Cancellation errors are NEVER
// classified as transient — the funnel propagates them immediately.
func IsTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// net.OpError covers most low-level transport failures.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	// DNS lookup failures.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// url.Error wraps everything from net/http; unwrap and re-classify.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Err != nil && urlErr.Err != err {
			return IsTransientNetworkError(urlErr.Err)
		}
		// Heuristic fallback for older Go versions where Timeout() is the
		// only signal.
		if urlErr.Timeout() {
			return true
		}
	}
	// Fallback heuristic on the message — covers the noisier corners of
	// net/http (proxy errors, "use of closed network connection", etc.).
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"i/o timeout",
		"timeout",
		"no such host",
		"proxy",
		"eof",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// Sleep blocks for the given delay or returns ctx.Err() if the context is
// cancelled first. Never panics.
func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Do invokes fn up to p.MaxAttempts times, sleeping the configured delay
// between attempts.
//
// fn returns (Outcome, error). On OutcomeSuccess Do returns (attempt, nil).
// On OutcomeFatal Do returns (attempt, fn's error). On OutcomeRetry Do
// sleeps and tries again, up to MaxAttempts; the sleep is the policy backoff,
// or the delay of a [WithDelay] hint when fn supplied one. After exhaustion Do
// returns (MaxAttempts, the last error). Any other Outcome value is fatal:
// Do returns (attempt, fn's error), or an *UnknownOutcomeError if that is nil.
//
// Cancellation is checked before each attempt and during the inter-attempt
// sleep; ctx.Err() is returned immediately if observed.
func Do(ctx context.Context, p Policy, fn func(attempt int) (Outcome, error)) (int, error) {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return attempt, err
		}
		outcome, err := fn(attempt)
		err, hint := splitHint(err)
		switch outcome {
		case OutcomeSuccess:
			return attempt, nil
		case OutcomeFatal:
			return attempt, err
		case OutcomeRetry:
			lastErr = err
			if attempt == p.MaxAttempts {
				return attempt, lastErr
			}
			delay := p.nextDelay(attempt)
			if hint > 0 {
				delay = hint
			}
			if sleepErr := Sleep(ctx, delay); sleepErr != nil {
				return attempt, sleepErr
			}
		default:
			// An outcome Do does not know is fatal: never loop on it.
			if err == nil {
				err = &UnknownOutcomeError{Outcome: outcome}
			}
			return attempt, err
		}
	}
	return p.MaxAttempts, lastErr
}
