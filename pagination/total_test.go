package pagination_test

import (
	"context"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
)

// A server that caps limit below the requested page size returns a short
// first page; the iterator stops there (short-page rule) and Total exposes
// the server's totalCount so the caller can detect the truncation.
func TestTotalExposedAfterShortPageTruncation(t *testing.T) {
	total := 0
	fetch := func(_ context.Context, off, limit int) ([]int, error) {
		total = 7
		n := 3 // server cap below limit
		out := make([]int, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, off+i)
		}
		return out, nil
	}
	it := pagination.NewWithOptions(pagination.Config{PageSize: 5}, fetch,
		pagination.Options[int]{Total: func() (int, bool) { return total, total > 0 }})
	if _, ok := it.Total(); ok {
		t.Fatal("Total known before the first fetch")
	}
	got, err := pagination.Collect(context.Background(), it)
	if err != nil || len(got) != 3 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	n, ok := it.Total()
	if !ok || n != 7 {
		t.Fatalf("Total() = %d,%v want 7,true", n, ok)
	}
}

func TestTotalUnknownWithoutOption(t *testing.T) {
	it := pagination.New(pagination.Config{PageSize: 2}, func(context.Context, int, int) ([]int, error) { return []int{1}, nil })
	if _, err := pagination.Collect(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	if _, ok := it.Total(); ok {
		t.Fatal("Total known without Options.Total")
	}
}
