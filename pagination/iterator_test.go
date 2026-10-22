package pagination_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
)

// pages models a fake upstream as a slice-of-slices: each inner slice is one
// page. The fetcher closes over this and returns the requested page (or the
// empty slice past the end).
func pageFetcher(pages [][]int) pagination.PageFetcher[int] {
	return func(ctx context.Context, offset, limit int) ([]int, error) {
		// offset is in items (offsetByCount=true) — convert to page index.
		if limit == 0 {
			limit = 1
		}
		idx := offset / limit
		if idx < 0 || idx >= len(pages) {
			return nil, nil
		}
		return pages[idx], nil
	}
}

func TestIteratorWalksMultiplePagesUntilEmpty(t *testing.T) {
	pages := [][]int{
		{1, 2, 3},
		{4, 5, 6},
		{7},
		{}, // empty page → stop
	}
	it := pagination.New(pagination.Config{
		PageSize:        3,
		OffsetIsByCount: true,
	}, pageFetcher(pages))

	got, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 3, 4, 5, 6, 7}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("idx %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestIteratorStopsAfterEmptyPage(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, offset, limit int) ([]int, error) {
		calls++
		if calls == 1 {
			return []int{1, 2, 3}, nil
		}
		return nil, nil
	}
	it := pagination.New(pagination.Config{PageSize: 3, OffsetIsByCount: true}, fetch)
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || calls != 2 {
		t.Errorf("collected %v, calls=%d", all, calls)
	}
	// After exhaustion, further Next calls keep returning (zero, false, nil).
	_, ok, err := it.Next(context.Background())
	if ok || err != nil {
		t.Errorf("post-exhaustion: ok=%v err=%v", ok, err)
	}
}

func TestIteratorOffsetByCountIncrementsCorrectly(t *testing.T) {
	var seenOffsets []int
	fetch := func(ctx context.Context, offset, limit int) ([]int, error) {
		seenOffsets = append(seenOffsets, offset)
		switch offset {
		case 0:
			return []int{1, 2, 3, 4, 5}, nil
		case 5:
			return []int{6, 7}, nil
		default:
			return nil, nil
		}
	}
	it := pagination.New(pagination.Config{PageSize: 5, OffsetIsByCount: true}, fetch)
	if _, err := pagination.Collect(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	want := []int{0, 5, 10}
	if len(seenOffsets) != 3 || seenOffsets[0] != want[0] || seenOffsets[1] != want[1] || seenOffsets[2] != want[2] {
		t.Errorf("offsets = %v, want %v", seenOffsets, want)
	}
}

func TestIteratorOffsetByPageNumberIncrementsCorrectly(t *testing.T) {
	var seenOffsets []int
	fetch := func(ctx context.Context, offset, limit int) ([]int, error) {
		seenOffsets = append(seenOffsets, offset)
		switch offset {
		case 0:
			return []int{1, 2}, nil
		case 1:
			return []int{3, 4}, nil
		case 2:
			return []int{5}, nil
		default:
			return nil, nil
		}
	}
	it := pagination.New(pagination.Config{
		PageSize:        2,
		OffsetIsByCount: false, // page-number style
	}, fetch)
	got, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("got %v", got)
	}
	want := []int{0, 1, 2, 3}
	if len(seenOffsets) != len(want) {
		t.Fatalf("offsets = %v, want %v", seenOffsets, want)
	}
	for i := range want {
		if seenOffsets[i] != want[i] {
			t.Errorf("idx %d: got %d, want %d", i, seenOffsets[i], want[i])
		}
	}
}

func TestIteratorTransientErrorIsRetried(t *testing.T) {
	var attempts int32
	fetch := func(ctx context.Context, offset, limit int) ([]int, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return nil, errors.New("transient")
		}
		return []int{1, 2}, nil
	}
	it := pagination.New(pagination.Config{
		PageSize:       2,
		PageRetriesMax: 5,
		PageRetryDelay: pagination.NoRetryDelay,
	}, fetch)
	first, ok, err := it.Next(context.Background())
	if err != nil || !ok || first != 1 {
		t.Fatalf("first item: %d ok=%v err=%v", first, ok, err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestIteratorRetryExhaustionPropagatesError(t *testing.T) {
	want := errors.New("never works")
	fetch := func(ctx context.Context, offset, limit int) ([]int, error) {
		return nil, want
	}
	it := pagination.New(pagination.Config{
		PageSize:       1,
		PageRetriesMax: 2,
		PageRetryDelay: pagination.NoRetryDelay,
	}, fetch)
	_, ok, err := it.Next(context.Background())
	if ok {
		t.Errorf("ok = true, want false")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestIteratorFatalClassifierShortCircuitsRetries(t *testing.T) {
	var attempts int32
	want := errors.New("permanent")
	fetch := func(ctx context.Context, offset, limit int) ([]int, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, want
	}
	it := pagination.New(pagination.Config{
		PageSize:       1,
		PageRetriesMax: 5,
		PageRetryDelay: 0,
		Retryable:      func(err error) bool { return false }, // never retry
	}, fetch)
	_, _, err := it.Next(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retries)", got)
	}
}

func TestIteratorContextCancellation(t *testing.T) {
	pages := [][]int{
		{1, 2},
		{3, 4},
		{5, 6},
		{}, // never reached
	}
	it := pagination.New(pagination.Config{PageSize: 2, OffsetIsByCount: true}, pageFetcher(pages))
	ctx, cancel := context.WithCancel(context.Background())

	// Drain first page.
	if _, ok, err := it.Next(ctx); !ok || err != nil {
		t.Fatal(err)
	}
	if _, ok, err := it.Next(ctx); !ok || err != nil {
		t.Fatal(err)
	}
	// Cancel before fetching the second page.
	cancel()
	_, ok, err := it.Next(ctx)
	if ok {
		t.Errorf("ok = true, want false on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestIteratorRangeOverFunc(t *testing.T) {
	pages := [][]int{{1, 2}, {3}, {}}
	it := pagination.New(pagination.Config{PageSize: 2, OffsetIsByCount: true}, pageFetcher(pages))

	var got []int
	for v, err := range it.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("got %v", got)
	}
}

func TestIteratorRangeOverFuncStopsOnBreak(t *testing.T) {
	pages := [][]int{{1, 2, 3}, {4, 5, 6}, {}}
	it := pagination.New(pagination.Config{PageSize: 3, OffsetIsByCount: true}, pageFetcher(pages))

	var got []int
	for v, err := range it.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
		if len(got) == 2 {
			break
		}
	}
	if len(got) != 2 {
		t.Errorf("got %v, want 2 elements", got)
	}
}

func TestCollectDrainsEverything(t *testing.T) {
	pages := [][]int{{1, 2}, {3, 4, 5}, {}}
	it := pagination.New(pagination.Config{PageSize: 5, OffsetIsByCount: true}, pageFetcher(pages))
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Errorf("got %v", all)
	}
}

func TestIteratorEmptyOnFirstPage(t *testing.T) {
	fetch := func(ctx context.Context, offset, limit int) ([]int, error) {
		return nil, nil
	}
	it := pagination.New(pagination.Config{PageSize: 10}, fetch)
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("got %v", all)
	}
}

func TestOffsetModeOverridesOffsetIsByCount(t *testing.T) {
	run := func(cfg pagination.Config) []int {
		var offsets []int
		fetch := func(_ context.Context, offset, _ int) ([]int, error) {
			offsets = append(offsets, offset)
			if len(offsets) >= 3 {
				return nil, nil
			}
			return []int{1}, nil
		}
		if _, err := pagination.Collect(context.Background(), pagination.New(cfg, fetch)); err != nil {
			t.Fatal(err)
		}
		return offsets
	}
	pages := run(pagination.Config{PageSize: 50, OffsetMode: pagination.OffsetModePages})
	if pages[0] != 0 || pages[1] != 1 || pages[2] != 2 {
		t.Errorf("pages mode offsets = %v", pages)
	}
	items := run(pagination.Config{PageSize: 50, OffsetMode: pagination.OffsetModeItems})
	if items[1] != 50 || items[2] != 100 {
		t.Errorf("items mode offsets = %v", items)
	}
}
