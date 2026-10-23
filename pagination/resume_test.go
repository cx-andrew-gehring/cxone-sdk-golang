package pagination_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// A caller that owns page retries (Retryable always false) calls Next again
// after an error: the iterator re-fetches the same offset and continues.
func TestNextAfterErrorRetriesSamePage(t *testing.T) {
	var offsets []int
	failOnce := true
	fetch := func(_ context.Context, off, limit int) ([]int, error) {
		offsets = append(offsets, off)
		if off == 2 && failOnce {
			failOnce = false
			return nil, errors.New("boom")
		}
		if off >= 4 {
			return []int{off}, nil // short page: end
		}
		return []int{off, off + 1}, nil
	}
	it := pagination.New(pagination.Config{PageSize: 2, OffsetMode: pagination.OffsetModeItems,
		Retryable: func(error) bool { return false }}, fetch)
	var got []int
	for {
		v, ok, err := it.Next(context.Background())
		if err != nil {
			continue // caller-owned retry: call Next again
		}
		if !ok {
			break
		}
		got = append(got, v)
	}
	if want := []int{0, 1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if want := []int{0, 2, 2, 4}; !reflect.DeepEqual(offsets, want) {
		t.Fatalf("offsets %v want %v", offsets, want)
	}
}
