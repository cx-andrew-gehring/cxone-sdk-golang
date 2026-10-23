package scans_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

// fakeResultsServer is a tiny in-process /api/results stand-in. Each test
// seeds it with two scan-id → result-list maps (old / new) and every
// GET /api/results returns the matching slice on a single page. Pagination
// is honored by returning an empty page on offset>0.
type fakeResultsServer struct {
	t      *testing.T
	oldID  string
	newID  string
	oldRes []*models.ScanResult
	newRes []*models.ScanResult
}

func (f *fakeResultsServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/results" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		scanID := q.Get("scan-id")
		offset, _ := strconv.Atoi(q.Get("offset"))
		var rs []*models.ScanResult
		switch scanID {
		case f.oldID:
			rs = f.oldRes
		case f.newID:
			rs = f.newRes
		default:
			f.t.Errorf("unexpected scan-id: %q", scanID)
		}
		if state := q.Get("state"); state != "" {
			filtered := make([]*models.ScanResult, 0, len(rs))
			for _, e := range rs {
				if e.State == state {
					filtered = append(filtered, e)
				}
			}
			rs = filtered
		}
		if offset > 0 {
			rs = nil
		}
		coll := models.ScanResultsCollection{Results: rs, TotalCount: uint(len(rs)), ScanID: scanID}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(coll)
	}
}

func TestCompareCountsNewResolvedRecurrentBySeverity(t *testing.T) {
	// Old: 1 HIGH (sim-a), 2 MEDIUM (sim-b, sim-c), 1 LOW (sim-d)
	// New: 3 HIGH (sim-a,x,y), 1 MEDIUM (sim-b), 1 LOW (sim-d), 1 INFO (sim-z)
	// Expected:
	//   HIGH:   New=2, Resolved=0, Recurrent=1
	//   MEDIUM: New=0, Resolved=1, Recurrent=1
	//   LOW:    New=0, Resolved=0, Recurrent=1
	//   INFO:   New=1, Resolved=0, Recurrent=0
	fake := &fakeResultsServer{
		t:     t,
		oldID: "old-1",
		newID: "new-1",
		oldRes: []*models.ScanResult{
			{ID: "1", SimilarityID: "sim-a", Severity: "HIGH"},
			{ID: "2", SimilarityID: "sim-b", Severity: "MEDIUM"},
			{ID: "3", SimilarityID: "sim-c", Severity: "MEDIUM"},
			{ID: "4", SimilarityID: "sim-d", Severity: "LOW"},
		},
		newRes: []*models.ScanResult{
			{ID: "5", SimilarityID: "sim-a", Severity: "HIGH"},
			{ID: "6", SimilarityID: "sim-x", Severity: "HIGH"},
			{ID: "7", SimilarityID: "sim-y", Severity: "HIGH"},
			{ID: "8", SimilarityID: "sim-b", Severity: "MEDIUM"},
			{ID: "9", SimilarityID: "sim-d", Severity: "LOW"},
			{ID: "10", SimilarityID: "sim-z", Severity: "INFO"},
		},
	}
	b := newBackend(t, fake.handler())

	cmp, err := b.Compare(context.Background(), fake.oldID, fake.newID)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if cmp.OldScanID != fake.oldID || cmp.NewScanID != fake.newID {
		t.Errorf("scan ids = %q/%q", cmp.OldScanID, cmp.NewScanID)
	}
	if cmp.Identity != string(models.IdentityKeySimilarityID) {
		t.Errorf("default identity = %q", cmp.Identity)
	}
	checkBucket := func(sev string, want models.ScanComparisonBucket) {
		t.Helper()
		if cmp.BySeverity[sev] != want {
			t.Errorf("%s = %+v, want %+v", sev, cmp.BySeverity[sev], want)
		}
	}
	checkBucket("HIGH", models.ScanComparisonBucket{New: 2, Resolved: 0, Recurrent: 1})
	checkBucket("MEDIUM", models.ScanComparisonBucket{New: 0, Resolved: 1, Recurrent: 1})
	checkBucket("LOW", models.ScanComparisonBucket{New: 0, Resolved: 0, Recurrent: 1})
	checkBucket("INFO", models.ScanComparisonBucket{New: 1, Resolved: 0, Recurrent: 0})
	wantTotals := models.ScanComparisonBucket{New: 3, Resolved: 1, Recurrent: 3}
	if cmp.Totals != wantTotals {
		t.Errorf("Totals = %+v, want %+v", cmp.Totals, wantTotals)
	}
}

func TestCompareWithIdentityKeyResultHash(t *testing.T) {
	// Same SimilarityID across scans but different ResultHash: with
	// ResultHash identity, these are NOT recurrent.
	fake := &fakeResultsServer{
		t:     t,
		oldID: "o",
		newID: "n",
		oldRes: []*models.ScanResult{
			{SimilarityID: "sim", Severity: "HIGH", ScanResultData: models.ScanResultData{ResultHash: "hash-old"}},
		},
		newRes: []*models.ScanResult{
			{SimilarityID: "sim", Severity: "HIGH", ScanResultData: models.ScanResultData{ResultHash: "hash-new"}},
		},
	}
	b := newBackend(t, fake.handler())
	cmp, err := b.Compare(context.Background(), fake.oldID, fake.newID, scans.WithIdentityKey(models.IdentityKeyResultHash))
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Identity != string(models.IdentityKeyResultHash) {
		t.Errorf("identity = %q", cmp.Identity)
	}
	want := models.ScanComparisonBucket{New: 1, Resolved: 1, Recurrent: 0}
	if cmp.BySeverity["HIGH"] != want {
		t.Errorf("HIGH = %+v, want %+v", cmp.BySeverity["HIGH"], want)
	}
}

func TestCompareGroupsNotExploitableFromNewScanByQuery(t *testing.T) {
	fake := &fakeResultsServer{
		t:     t,
		oldID: "o",
		newID: "n",
		oldRes: []*models.ScanResult{
			{SimilarityID: "a", Severity: "HIGH", State: "TO_VERIFY", ScanResultData: models.ScanResultData{QueryName: "SQL_Injection"}},
		},
		newRes: []*models.ScanResult{
			{SimilarityID: "a", Severity: "HIGH", State: "NOT_EXPLOITABLE", ScanResultData: models.ScanResultData{QueryName: "SQL_Injection"}},
			{SimilarityID: "b", Severity: "MEDIUM", State: "NOT_EXPLOITABLE", ScanResultData: models.ScanResultData{QueryName: "SQL_Injection"}},
			{SimilarityID: "c", Severity: "MEDIUM", State: "NOT_EXPLOITABLE", ScanResultData: models.ScanResultData{QueryName: "XSS"}},
			{SimilarityID: "d", Severity: "LOW", State: "TO_VERIFY", ScanResultData: models.ScanResultData{QueryName: "Other"}},
		},
	}
	b := newBackend(t, fake.handler())
	cmp, err := b.Compare(context.Background(), fake.oldID, fake.newID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.NotExploitable) != 2 {
		t.Fatalf("NotExploitable = %+v", cmp.NotExploitable)
	}
	groups := map[string]models.NotExploitableGroup{}
	for _, g := range cmp.NotExploitable {
		groups[g.QueryName] = g
	}
	if groups["SQL_Injection"].Count != 2 {
		t.Errorf("SQL_Injection count = %d, want 2", groups["SQL_Injection"].Count)
	}
	if groups["XSS"].Count != 1 {
		t.Errorf("XSS count = %d, want 1", groups["XSS"].Count)
	}
}

func TestCompareValidatesScanIDs(t *testing.T) {
	b := newBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called: %s", r.URL.Path)
	}))
	cases := []struct {
		oldID, newID string
		wantField    string
	}{
		{"", "n", "oldScanID"},
		{"o", "", "newScanID"},
	}
	for _, c := range cases {
		_, err := b.Compare(context.Background(), c.oldID, c.newID)
		var ce *cxerrors.ConfigurationError
		if !errors.As(err, &ce) || ce.Field != c.wantField {
			t.Errorf("(%q,%q): got %v, want ConfigurationError on %q", c.oldID, c.newID, err, c.wantField)
		}
	}
}

func TestCompareCancellationPropagates(t *testing.T) {
	releaseAll := make(chan struct{})
	gotRequest := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case gotRequest <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-releaseAll:
			coll := models.ScanResultsCollection{Results: nil, TotalCount: 0, ScanID: r.URL.Query().Get("scan-id")}
			_ = json.NewEncoder(w).Encode(coll)
		}
	}))
	defer srv.Close()
	defer close(releaseAll)

	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    srv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test",
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	b := &scans.Backend{Executor: exec, BaseURL: srv.URL + "/"}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel as soon as at least one request lands.
	go func() {
		<-gotRequest
		cancel()
	}()

	_, err := b.Compare(ctx, "o", "n")
	if err == nil {
		t.Fatal("expected an error after cancellation")
	}
	// The funnel may wrap context.Canceled in a CommunicationError or return
	// it verbatim depending on which goroutine wins the race. Accept either.
	if !errors.Is(err, context.Canceled) {
		var ce *cxerrors.CommunicationError
		if !errors.As(err, &ce) {
			t.Errorf("err = %v, want context.Canceled or *CommunicationError", err)
		}
	}
}
