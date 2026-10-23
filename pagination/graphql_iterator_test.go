package pagination_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

// fakeGQL is a minimal in-memory GraphQLExecutor used by the iterator
// tests. It captures the (skip, take) variables from each call so the test
// can verify the pagination math, and returns canned data for the matching
// page.
type fakeGQL struct {
	pages          [][]int
	gotSkipTake    [][2]int
	overrideSkipKey string
	overrideTakeKey string
}

func (f *fakeGQL) Execute(_ context.Context, _ string, vars map[string]any, out any) error {
	skipKey := "skip"
	takeKey := "take"
	if f.overrideSkipKey != "" {
		skipKey = f.overrideSkipKey
	}
	if f.overrideTakeKey != "" {
		takeKey = f.overrideTakeKey
	}
	skip, ok1 := vars[skipKey].(int)
	take, ok2 := vars[takeKey].(int)
	if !ok1 || !ok2 {
		return errors.New("missing skip/take variables")
	}
	f.gotSkipTake = append(f.gotSkipTake, [2]int{skip, take})

	idx := skip / take
	if idx < 0 || idx >= len(f.pages) {
		// End of stream → return an empty data envelope.
		raw := json.RawMessage(`{"items":[]}`)
		return json.Unmarshal(raw, out)
	}
	type wire struct {
		Items []int `json:"items"`
	}
	w := wire{Items: f.pages[idx]}
	b, _ := json.Marshal(w)
	return json.Unmarshal(b, out)
}

func decodeItems(data json.RawMessage) ([]int, error) {
	var wrapper struct {
		Items []int `json:"items"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Items, nil
}

func TestGraphQLIteratorWalksMultiplePages(t *testing.T) {
	gql := &fakeGQL{
		pages: [][]int{
			{1, 2, 3},
			{4, 5, 6},
			{7},
		},
	}
	it := pagination.NewGraphQL[int](
		gql,
		"query ($skip: Int!, $take: Int!) { items(skip: $skip, take: $take) { ... } }",
		map[string]any{"projectId": "p1"},
		decodeItems,
		pagination.Config{PageSize: 3, PageRetryDelay: pagination.NoRetryDelay},
		pagination.SkipTakeNames{},
	)
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 3, 4, 5, 6, 7}
	if len(all) != len(want) {
		t.Fatalf("got %v, want %v", all, want)
	}
	wantSkipTake := [][2]int{{0, 3}, {3, 3}, {6, 3}}
	if len(gql.gotSkipTake) != len(wantSkipTake) {
		t.Fatalf("calls = %v, want %v", gql.gotSkipTake, wantSkipTake)
	}
	for i := range wantSkipTake {
		if gql.gotSkipTake[i] != wantSkipTake[i] {
			t.Errorf("call %d: got %v, want %v", i, gql.gotSkipTake[i], wantSkipTake[i])
		}
	}
}

func TestGraphQLIteratorRespectsCustomVarNames(t *testing.T) {
	gql := &fakeGQL{
		pages:           [][]int{{1, 2}},
		overrideSkipKey: "offset",
		overrideTakeKey: "first",
	}
	it := pagination.NewGraphQL[int](
		gql,
		"query ($offset: Int!, $first: Int!) { items(offset: $offset, first: $first) { ... } }",
		nil,
		decodeItems,
		pagination.Config{PageSize: 2, PageRetryDelay: pagination.NoRetryDelay},
		pagination.SkipTakeNames{Skip: "offset", Take: "first"},
	)
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("got %v", all)
	}
}

func TestGraphQLIteratorPropagatesExecutorError(t *testing.T) {
	want := errors.New("upstream busted")
	gql := executorFunc(func(_ context.Context, _ string, _ map[string]any, _ any) error {
		return want
	})
	it := pagination.NewGraphQL[int](
		gql,
		"q",
		nil,
		decodeItems,
		pagination.Config{PageSize: 1, PageRetriesMax: 1, PageRetryDelay: pagination.NoRetryDelay},
		pagination.SkipTakeNames{},
	)
	_, _, err := it.Next(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

// executorFunc adapts a plain function to the GraphQLExecutor interface so
// individual tests can supply behavior inline.
type executorFunc func(ctx context.Context, query string, vars map[string]any, out any) error

func (f executorFunc) Execute(ctx context.Context, query string, vars map[string]any, out any) error {
	return f(ctx, query, vars, out)
}
