package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An Outcome value Do does not know is fatal: one call, no sleep, and a
// non-nil error even when fn returned nil.
func TestDoUnknownOutcomeIsFatalWithTypedError(t *testing.T) {
	calls := 0
	attempt, err := Do(context.Background(), Policy{MaxAttempts: 3, MaxDelay: time.Hour}, func(int) (Outcome, error) {
		calls++
		return Outcome(99), nil
	})
	if calls != 1 || attempt != 1 {
		t.Fatalf("calls=%d attempt=%d, want 1/1", calls, attempt)
	}
	var ue *UnknownOutcomeError
	if !errors.As(err, &ue) || ue.Outcome != Outcome(99) {
		t.Fatalf("err=%#v, want *UnknownOutcomeError{99}", err)
	}
}

func TestDoUnknownOutcomeReturnsFnError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	attempt, err := Do(context.Background(), Policy{MaxAttempts: 3, MaxDelay: time.Hour}, func(int) (Outcome, error) {
		calls++
		return Outcome(-1), boom
	})
	if calls != 1 || attempt != 1 || !errors.Is(err, boom) {
		t.Fatalf("calls=%d attempt=%d err=%v", calls, attempt, err)
	}
}

// WithDelay overrides the policy backoff for the next sleep, and Do hands
// back the wrapped error (not the hint) on exhaustion.
func TestDoWithDelayOverridesBackoff(t *testing.T) {
	sentinel := errors.New("rate limited")
	calls := 0
	start := time.Now()
	attempt, err := Do(context.Background(), Policy{MaxAttempts: 3, MaxDelay: time.Hour}, func(int) (Outcome, error) {
		calls++
		return OutcomeRetry, WithDelay(sentinel, 5*time.Millisecond)
	})
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("slept the policy backoff (%s), not the hint", el)
	}
	if calls != 3 || attempt != 3 {
		t.Fatalf("calls=%d attempt=%d", calls, attempt)
	}
	if err != sentinel {
		t.Fatalf("err=%#v, want the unwrapped sentinel", err)
	}
}

func TestDoWithDelayIsContextAware(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	_, err := Do(ctx, Policy{MaxAttempts: 3, MaxDelay: 0}, func(int) (Outcome, error) {
		calls++
		return OutcomeRetry, WithDelay(errors.New("x"), time.Hour)
	})
	if calls != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

// A non-positive hint falls back to the policy backoff (here 0: no sleep).
func TestDoWithDelayZeroUsesPolicy(t *testing.T) {
	calls := 0
	_, _ = Do(context.Background(), Policy{MaxAttempts: 2, MaxDelay: 0}, func(int) (Outcome, error) {
		calls++
		return OutcomeRetry, WithDelay(errors.New("x"), 0)
	})
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
