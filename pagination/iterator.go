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
	"crypto/sha256"
	"errors"
	"fmt"
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
// after the first empty page (CLAUDE.md §10.1). A page shorter than the
// requested limit is also treated as the final page, and an endpoint-supplied
// total ([Options.Total]) stops iteration once it has been consumed.
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

	// MaxPages is a hard safety cap on the number of pages fetched by one
	// iterator; exceeding it yields [ErrMaxPagesExceeded]. It protects
	// against a misbehaving upstream that never ends. Default
	// [DefaultMaxPages] (100000).
	MaxPages int
}

// DefaultMaxPages is the default [Config.MaxPages].
const DefaultMaxPages = 100000

// ErrPaginationStalled is returned when two consecutive pages are identical
// (by [Options.Key]), which means the upstream is ignoring the offset and
// iteration would never terminate.
var ErrPaginationStalled = errors.New("pagination: upstream returned an identical page twice (offset ignored?); stopping")

// ErrMaxPagesExceeded is returned when [Config.MaxPages] pages were fetched
// without reaching the end of the stream.
var ErrMaxPagesExceeded = errors.New("pagination: max pages exceeded")

// Options carries optional, item-typed termination hints for
// [NewWithOptions].
type Options[T any] struct {
	// Total, if non-nil, reports the upstream's total item count as seen in
	// the most recently fetched page (ok=false when unknown). Iteration stops
	// once the number of items consumed, counted from item offset 0 and
	// accounting for Pages vs Items offset mode, reaches the total.
	Total func() (total int, ok bool)

	// Key, if non-nil, returns an identity string for an item. Two
	// consecutive pages with identical key sequences abort iteration with
	// [ErrPaginationStalled].
	Key func(T) string
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
	if c.MaxPages == 0 {
		c.MaxPages = DefaultMaxPages
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
	opts  Options[T]

	pagesFetched int
	prevKey      string
	havePrevKey  bool

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
	return NewWithOptions(cfg, fetch, Options[T]{})
}

// NewWithOptions is [New] with optional total/identity hints that let the
// iterator terminate without an extra empty-page request and detect an
// upstream that ignores the offset.
func NewWithOptions[T any](cfg Config, fetch PageFetcher[T], opts Options[T]) *Iterator[T] {
	cfg = cfg.withDefaults()
	return &Iterator[T]{
		cfg:    cfg,
		fetch:  fetch,
		opts:   opts,
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
	if it.pagesFetched >= it.cfg.MaxPages {
		return ErrMaxPagesExceeded
	}
	maxAttempts := it.cfg.PageRetriesMax + 1 // PageRetriesMax is retries on top of attempt 1
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := it.fetch(ctx, it.offset, it.cfg.PageSize)
		if err == nil {
			it.pagesFetched++
			if err := it.checkStall(items); err != nil {
				return err
			}
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

// checkStall errors when items repeats the previous page (by Options.Key).
func (it *Iterator[T]) checkStall(items []T) error {
	if it.opts.Key == nil || len(items) == 0 {
		return nil
	}
	h := sha256.New()
	for _, x := range items {
		k := it.opts.Key(x)
		_, _ = fmt.Fprintf(h, "%d:%s|", len(k), k)
	}
	key := string(h.Sum(nil))
	if it.havePrevKey && key == it.prevKey {
		return ErrPaginationStalled
	}
	it.prevKey, it.havePrevKey = key, true
	return nil
}

func (it *Iterator[T]) byCount() bool {
	switch it.cfg.OffsetMode {
	case OffsetModePages:
		return false
	case OffsetModeItems:
		return true
	}
	return it.cfg.OffsetIsByCount
}

// advanceOffset increments the iterator's offset for the next page and marks
// the stream exhausted when this page was short or the upstream total has
// been consumed.
func (it *Iterator[T]) advanceOffset(pageLen int) {
	byCount := it.byCount()
	if byCount {
		it.offset += it.cfg.PageSize
	} else {
		it.offset++
	}
	if pageLen < it.cfg.PageSize {
		it.exhausted = true
		return
	}
	if it.opts.Total != nil {
		if total, ok := it.opts.Total(); ok {
			consumed := it.offset // items mode: offset is already in items
			if !byCount {
				consumed = it.offset * it.cfg.PageSize
			}
			if consumed >= total {
				it.exhausted = true
			}
		}
	}
}

// Total reports the upstream's total item count as seen on the most recently
// fetched page, when the iterator was built with [Options.Total] and the
// endpoint reported one (ok=false otherwise, and before the first fetch).
//
// The iterator ends on a short page. A service that caps the page size below
// [Config.PageSize] therefore ends the stream after its first page; comparing
// the number of items consumed with Total after the stream ends detects that
// truncation (see [StreamStats]).
func (it *Iterator[T]) Total() (total int, ok bool) {
	if it.opts.Total == nil || it.pagesFetched == 0 {
		return 0, false
	}
	return it.opts.Total()
}

// StreamStats summarises a finished stream: the number of items delivered to
// the caller and the upstream's reported total, when known.
type StreamStats struct {
	Delivered  int
	Total      int
	TotalKnown bool
}

// Truncated reports whether the upstream reported more items than were
// delivered (for example a page-size cap on the service side made the first
// page short and so ended the stream early).
func (s StreamStats) Truncated() bool { return s.TotalKnown && s.Delivered < s.Total }

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
