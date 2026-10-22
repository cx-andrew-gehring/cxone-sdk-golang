package pagination_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
)

func TestStopsOnShortPage(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _, limit int) ([]int, error) {
		calls++
		if calls > 50 {
			t.Fatal("runaway iteration")
		}
		return []int{1}, nil // always 1 item, limit is 10 → short
	}
	got, err := pagination.Collect(context.Background(), pagination.New(pagination.Config{PageSize: 10}, fetch))
	if err != nil || len(got) != 1 || calls != 1 {
		t.Fatalf("got=%v err=%v calls=%d", got, err, calls)
	}
}

func TestTotalExitOnExactMultiple(t *testing.T) {
	var offsets []int
	fetch := func(_ context.Context, off, limit int) ([]int, error) {
		offsets = append(offsets, off)
		if len(offsets) > 10 {
			t.Fatal("runaway")
		}
		return []int{1, 2}, nil
	}
	opts := pagination.Options[int]{Total: func() (int, bool) { return 4, true }}
	for _, mode := range []pagination.OffsetMode{pagination.OffsetModeItems, pagination.OffsetModePages} {
		offsets = nil
		it := pagination.NewWithOptions(pagination.Config{PageSize: 2, OffsetMode: mode}, fetch, opts)
		got, err := pagination.Collect(context.Background(), it)
		if err != nil || len(got) != 4 || len(offsets) != 2 {
			t.Fatalf("mode %v got=%v err=%v offsets=%v", mode, got, err, offsets)
		}
		want := []int{0, 2}
		if mode == pagination.OffsetModePages {
			want = []int{0, 1}
		}
		if offsets[0] != want[0] || offsets[1] != want[1] {
			t.Errorf("mode %v offsets = %v want %v", mode, offsets, want)
		}
	}
}

func TestStallGuardIdenticalPages(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _, _ int) ([]int, error) {
		calls++
		return []int{7, 8}, nil // ignores offset, full page
	}
	it := pagination.NewWithOptions(pagination.Config{PageSize: 2}, fetch,
		pagination.Options[int]{Key: func(i int) string { return strconv.Itoa(i) }})
	_, err := pagination.Collect(context.Background(), it)
	if !errors.Is(err, pagination.ErrPaginationStalled) {
		t.Fatalf("err = %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d", calls)
	}
}

func TestMaxPagesCap(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _, _ int) ([]int, error) {
		calls++
		return []int{calls}, nil
	}
	it := pagination.New(pagination.Config{PageSize: 1, MaxPages: 5}, fetch)
	_, err := pagination.Collect(context.Background(), it)
	if !errors.Is(err, pagination.ErrMaxPagesExceeded) || calls != 5 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
