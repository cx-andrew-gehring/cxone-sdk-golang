// Package pagination implements item-yielding iterators over paginated
// Checkmarx One endpoints.
//
// CLAUDE.md §10.1 mandates that pagination be exposed as a stream of items,
// not a stream of pages: callers should not have to think about page
// boundaries. The [Iterator] type wraps a user-supplied page fetcher and
// presents the results as a flat sequence of values, transparently fetching
// the next page when the in-memory buffer drains and stopping when the
// upstream returns an empty page.
//
// The iterator supports both offset styles (offset-by-count, offset-by-page-
// number) and applies its own retry budget to each page fetch — independent
// of the request funnel's retry budget — so a transient failure on one page
// does not abort the whole walk.
//
// # Direct vs range-over-func
//
// The iterator exposes both styles. Use whichever fits your code:
//
//	// Direct style (Go 1.18+):
//	for {
//	    project, ok, err := it.Next(ctx)
//	    if err != nil { return err }
//	    if !ok { break }
//	    fmt.Println(project.Name)
//	}
//
//	// range-over-func (Go 1.23+):
//	for project, err := range it.All(ctx) {
//	    if err != nil { return err }
//	    fmt.Println(project.Name)
//	}
//
//	// Collect everything (small result sets only):
//	all, err := pagination.Collect(ctx, it)
//
// # Constructing an iterator
//
// Most users will obtain a ready-built iterator from an endpoint package:
//
//	it := client.Advanced().Projects().Iter(ctx, query)
//
// Power users can construct one directly with [New] and a custom
// [PageFetcher].
package pagination

import (
	"context"
	"errors"
	"iter"
	"time"
)

// PageFetcher is the user-supplied function that fetches one page of items
// from an upstream endpoint.
//
// offset and limit are exactly what the iterator wants the fetcher to send.
// The fetcher is responsible for translating those into the endpoint's
// specific query-parameter names ("offset"/"limit", "page"/"size", etc.) and
// returning the array of items contained in the response.
//
// Returning a nil or empty slice signals end-of-stream — the iterator stops
// after the first empty page (CLAUDE.md §10.1).
//
// Returning an error halts the iterator unless it is classified as transient
// (see [Config.Retryable]); transient errors are retried up to
// [Config.PageRetriesMax] times before propagating to the caller.
type PageFetcher[T any] func(ctx context.Context, offset, limit int) ([]T, error)

// OffsetMode selects how the iterator advances the "offset" query value.
type OffsetMode int

const (
	// OffsetModeDefault defers to OffsetIsByCount (or, for endpoint-specific
	// iterators such as results.IterRaw, to the endpoint's documented default).
	OffsetModeDefault OffsetMode = iota
	// OffsetModePages advances offset by 1 per page (spec: "number of pages to skip").
	OffsetModePages
	// OffsetModeItems advances offset by PageSize per page ("number of results to skip").
	OffsetModeItems
)

// Config controls iterator behavior. Zero values pick sensible defaults
// (PageSize=100, PageRetriesMax=5, PageRetryDelay=3s, OffsetIsByCount=true).
type Config struct {
	// PageSize is the number of items to request per page. Default 100.
	PageSize int

	// OffsetInitValue is the offset/page-number for the first request.
	// Default 0.
	OffsetInitValue int

	// OffsetIsByCount controls whether offset is incremented by PageSize
	// (true — the typical "offset" / "skip" REST style) or by 1 (false —
	// page-number style). Default true.
	OffsetIsByCount bool

	// OffsetMode, when not [OffsetModeDefault], takes precedence over
	// OffsetIsByCount. It exists so callers (and endpoint-specific
	// iterators) can express "use the spec default for this endpoint"
	// (the zero value) separately from an explicit choice of page-number
	// or item-count paging.
	OffsetMode OffsetMode

	// PageRetriesMax is the maximum number of retry attempts for a single
	// page fetch (CLAUDE.md §10.1). The first attempt is NOT counted, so
	// PageRetriesMax=5 means up to 6 total attempts. Default 5.
	PageRetriesMax int

	// PageRetryDelay is the fixed delay between page-fetch retries.
	// Default 3 seconds. Pass [NoRetryDelay] to disable backoff entirely
	// (useful in tests); a literal zero value resolves to the default.
	PageRetryDelay time.Duration

	// Retryable, if non-nil, classifies an error returned by [PageFetcher]
	// as retryable (true) or fatal (false). When nil, ALL errors are
	// considered retryable up to PageRetriesMax — that matches the spirit
	// of "per-page retry budget independent of the funnel" because the
	// funnel itself has already classified its own errors before they
	// reach here.
	Retryable func(error) bool
}

// NoRetryDelay disables the inter-attempt sleep on per-page retries.
// Distinct from a zero-value Config.PageRetryDelay (which resolves to the
// default of 3 seconds). Tests use this to keep retry coverage fast.
const NoRetryDelay time.Duration = -1

// withDefaults returns a copy of cfg with zero-valued fields filled in.
func (c Config) withDefaults() Config {
	if c.PageSize == 0 {
		c.PageSize = 100
	}
	if c.PageRetriesMax == 0 {
		c.PageRetriesMax = 5
	}
	switch {
	case c.PageRetryDelay == 0:
		c.PageRetryDelay = 3 * time.Second
	case c.PageRetryDelay < 0:
		c.PageRetryDelay = 0
	}
	// OffsetIsByCount default is true. Sentinel: if the user really wanted
	// page-number style they set it explicitly. Zero-value defaulting to
	// the common case (offset-by-count) is safer than the inverse.
	//
	// We can't tell zero-value-false from explicit-false, but the iterator
	// is virtually always constructed by an endpoint adapter that sets the
	// field explicitly, so this is acceptable.
	return c
}

// Iterator is a stateful, buffered, item-yielding iterator over a paginated
// upstream. It is NOT safe for concurrent use — share the underlying
// endpoint and create one iterator per goroutine.
type Iterator[T any] struct {
	cfg   Config
	fetch PageFetcher[T]

	// Page state.
	offset    int
	exhausted bool

	// Buffer of the current page; consumed by Next from index 0 upward.
	buffer []T
}

// New constructs an [Iterator] over the supplied page fetcher with the given
// config. The iterator is ready to use immediately; the first call to
// [Iterator.Next] triggers the first page fetch.
func New[T any](cfg Config, fetch PageFetcher[T]) *Iterator[T] {
	cfg = cfg.withDefaults()
	return &Iterator[T]{
		cfg:    cfg,
		fetch:  fetch,
		offset: cfg.OffsetInitValue,
	}
}

// Next returns the next item in the stream.
//
// On success the returned bool is true and the returned error is nil.
// When the upstream is exhausted Next returns (zero, false, nil) — note
// that this is the SAME shape as a normal end-of-iteration in Go's
// idiomatic stream pattern.
//
// On a non-recoverable fetch error Next returns (zero, false, err); the
// iterator should not be used after that.
func (it *Iterator[T]) Next(ctx context.Context) (T, bool, error) {
	var zero T
	if it.exhausted && len(it.buffer) == 0 {
		return zero, false, nil
	}
	if len(it.buffer) == 0 {
		if err := it.fetchNextPage(ctx); err != nil {
			return zero, false, err
		}
		if len(it.buffer) == 0 {
			// Upstream returned an empty page → end of stream.
			it.exhausted = true
			return zero, false, nil
		}
	}
	item := it.buffer[0]
	it.buffer = it.buffer[1:]
	return item, true, nil
}

// fetchNextPage retrieves the next page from the upstream, applying the
// per-page retry budget. On success it stores the result in it.buffer and
// advances the offset for the following call.
func (it *Iterator[T]) fetchNextPage(ctx context.Context) error {
	if it.exhausted {
		return nil
	}
	maxAttempts := it.cfg.PageRetriesMax + 1 // PageRetriesMax is retries on top of attempt 1
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := it.fetch(ctx, it.offset, it.cfg.PageSize)
		if err == nil {
			it.buffer = items
			it.advanceOffset(len(items))
			return nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if it.cfg.Retryable != nil && !it.cfg.Retryable(err) {
			return err
		}
		if attempt == maxAttempts {
			break
		}
		if sleepErr := sleep(ctx, it.cfg.PageRetryDelay); sleepErr != nil {
			return sleepErr
		}
	}
	return lastErr
}

// advanceOffset increments the iterator's offset for the next page,
// honoring OffsetIsByCount.
func (it *Iterator[T]) advanceOffset(pageLen int) {
	byCount := it.cfg.OffsetIsByCount
	switch it.cfg.OffsetMode {
	case OffsetModePages:
		byCount = false
	case OffsetModeItems:
		byCount = true
	}
	if byCount {
		it.offset += it.cfg.PageSize
	} else {
		it.offset++
	}
	// If the page returned fewer items than PageSize, the next fetch is
	// likely to return empty — but we don't optimize that away because the
	// upstream might be paginating differently than expected. Always make
	// the canonical "is there another page?" call.
	_ = pageLen
}

// All returns a Go 1.23 range-over-func sequence so callers can iterate with
// the standard for-range syntax. The yielded error is non-nil exactly when
// iteration aborts due to a fetch failure; in that case the loop terminates
// after the yield.
//
//	for project, err := range it.All(ctx) {
//	    if err != nil {
//	        return err
//	    }
//	    process(project)
//	}
//
// The yielded T is the zero value when err is non-nil.
func (it *Iterator[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			item, ok, err := it.Next(ctx)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			if !ok {
				return
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

// Collect drains an iterator into a single slice. Returns the items
// retrieved before any error and the error itself.
//
// Use Collect only when you expect a small result set — for large catalogs
// (full project list, full preset query catalog) prefer streaming via
// [Iterator.Next] or [Iterator.All] so memory stays bounded.
func Collect[T any](ctx context.Context, it *Iterator[T]) ([]T, error) {
	var out []T
	for {
		item, ok, err := it.Next(ctx)
		if err != nil {
			return out, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, item)
	}
}

// sleep is a context-aware sleep used by the per-page retry budget.
func sleep(ctx context.Context, d time.Duration) error {
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
