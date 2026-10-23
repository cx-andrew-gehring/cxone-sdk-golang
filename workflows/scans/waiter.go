package scans

import (
	"context"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/observability"
)

// WaitOptions configures [WaitUntilComplete].
type WaitOptions struct {
	// PollInterval is the time to wait between successive Refresh calls.
	// Default 10 seconds.
	PollInterval time.Duration

	// MaxDuration is an upper bound on total wait time. Zero means no
	// upper bound (cancellation is still honored via ctx).
	MaxDuration time.Duration

	// Logger receives one cxone.scan.poll event per Refresh call. Defaults
	// to [observability.NopLogger]. Pass the same logger you wired into
	// the cxone client for end-to-end visibility.
	Logger observability.Logger

	// SuppressFailure, when true, causes the waiter to return the final
	// ScanInspector with no error even on a terminal failed state, leaving
	// it to the caller to inspect Failed() / StateMessage() and decide
	// what to do. The zero-value default (false) causes the waiter to
	// return a *cxerrors.ScanError on terminal failure, which is the
	// behavior the typical caller wants.
	SuppressFailure bool
}

// WaitUntilComplete polls the supplied [ScanInspector] at PollInterval
// intervals until the scan reaches a terminal state (Successful or Failed)
// and returns the final inspector.
//
// CLAUDE.md §9.4 — this is the "waiter convenience" the spec calls out.
// Cancellation is honored: if ctx is cancelled, the waiter returns the
// last-observed inspector and ctx.Err().
//
// On terminal failure with FailOnError=true (the default), WaitUntilComplete
// returns a *cxerrors.ScanError describing the failure mode; the inspector
// itself is still returned so callers can introspect it.
func WaitUntilComplete(ctx context.Context, insp *ScanInspector, opts WaitOptions) (*ScanInspector, error) {
	if insp == nil {
		return nil, &cxerrors.ConfigurationError{Field: "inspector", Reason: "is required"}
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 10 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = observability.NopLogger{}
	}
	deadline, hasDeadline := computeDeadline(ctx, opts.MaxDuration)

	for {
		// Inspect current state.
		opts.Logger.Info(ctx, observability.EventScanPoll,
			observability.F("scanId", insp.ID()),
			observability.F("status", string(insp.Status())),
			observability.F("stateMessage", insp.StateMessage()),
		)
		if insp.Successful() {
			return insp, nil
		}
		if insp.Failed() {
			if opts.SuppressFailure {
				return insp, nil
			}
			return insp, &cxerrors.ScanError{
				ScanID: insp.ID(),
				Reason: insp.StateMessage(),
			}
		}
		// Still executing — sleep and refresh.
		if err := sleepUntil(ctx, opts.PollInterval, deadline, hasDeadline); err != nil {
			return insp, err
		}
		if _, err := insp.Refresh(ctx); err != nil {
			return insp, err
		}
	}
}

// computeDeadline returns the absolute deadline at which the waiter should
// stop polling, or zero+false if there is no upper bound.
func computeDeadline(ctx context.Context, maxDur time.Duration) (time.Time, bool) {
	if maxDur > 0 {
		return time.Now().Add(maxDur), true
	}
	if d, ok := ctx.Deadline(); ok {
		return d, true
	}
	return time.Time{}, false
}

// sleepUntil blocks for the given interval, but returns early if ctx is
// cancelled or the absolute deadline is reached. Returns ctx.Err() on
// cancellation, or context.DeadlineExceeded if the maxDuration is hit.
func sleepUntil(ctx context.Context, interval time.Duration, deadline time.Time, hasDeadline bool) error {
	if hasDeadline {
		if remaining := time.Until(deadline); remaining < interval {
			interval = remaining
			if interval <= 0 {
				return context.DeadlineExceeded
			}
		}
	}
	t := time.NewTimer(interval)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
