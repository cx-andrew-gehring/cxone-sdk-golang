package scm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WaitOptions configures [WaitUntilComplete].
type WaitOptions struct {
	// PollInterval is the time between successive status polls.
	// Default 15 seconds.
	PollInterval time.Duration

	// MaxDuration is an upper bound on total wait time. Zero means no
	// limit (cancellation is still honored via ctx).
	//
	// Setting this is strongly recommended: an unrecognized migrationStatus
	// is treated as "still executing", so a conversion reporting a status
	// this SDK does not know about waits here until the bound is reached
	// rather than returning a possibly-wrong terminal verdict.
	MaxDuration time.Duration
}

// WaitUntilComplete polls the conversion process until it reaches a terminal
// state. Returns the final inspector.
//
// A conversion that ends with any project in failedProjectList is reported as
// an error, even when the process-level status is "OK" — see
// [ConversionInspector.Failed].
//
// Cancellation is honored: if ctx is cancelled, the waiter returns the
// last-observed inspector and ctx.Err().
func WaitUntilComplete(ctx context.Context, insp *ConversionInspector, opts WaitOptions) (*ConversionInspector, error) {
	if insp == nil {
		return nil, fmt.Errorf("scm: inspector is nil")
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 15 * time.Second
	}
	deadline, hasDeadline := computeDeadline(ctx, opts.MaxDuration)

	for {
		if insp.Completed() {
			return insp, nil
		}
		if insp.Failed() {
			return insp, conversionError(insp)
		}
		if err := sleepUntil(ctx, opts.PollInterval, deadline, hasDeadline); err != nil {
			return insp, err
		}
		if _, err := insp.Refresh(ctx); err != nil {
			return insp, err
		}
	}
}

// conversionError renders a terminal failure, naming the repos that failed so
// the caller does not have to reach back into the inspector to find out.
func conversionError(insp *ConversionInspector) error {
	if failed := insp.FailedProjects(); len(failed) > 0 {
		return fmt.Errorf("scm: conversion %s failed for %d project(s): %s",
			insp.ID(), len(failed), strings.Join(failed, ", "))
	}
	return fmt.Errorf("scm: conversion %s ended with status %q", insp.ID(), insp.Status())
}

func computeDeadline(ctx context.Context, maxDur time.Duration) (time.Time, bool) {
	if maxDur > 0 {
		return time.Now().Add(maxDur), true
	}
	if d, ok := ctx.Deadline(); ok {
		return d, true
	}
	return time.Time{}, false
}

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
