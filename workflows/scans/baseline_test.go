package scans_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

func scansHandler(list []models.ScanResponseModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		off, _ := strconv.Atoi(q.Get("offset"))
		lim, _ := strconv.Atoi(q.Get("limit"))
		if lim <= 0 {
			lim = len(list)
		}
		out := []models.ScanResponseModel{}
		if off < len(list) {
			end := off + lim
			if end > len(list) {
				end = len(list)
			}
			out = list[off:end]
		}
		_ = json.NewEncoder(w).Encode(models.ScansCollectionResponseModel{
			TotalCount: uint(len(list)), FilteredTotalCount: uint(len(list)), Scans: out,
		})
	}
}

func TestLatestCompletedSkipsRunning(t *testing.T) {
	var gotQuery map[string][]string
	h := scansHandler([]models.ScanResponseModel{
		{ID: "run", Status: models.ScanRunning, Branch: "main", ProjectID: "p1"},
		{ID: "fail", Status: models.ScanFailed, Branch: "main", ProjectID: "p1"},
		{ID: "part", Status: models.ScanPartial, Branch: "main", ProjectID: "p1"},
		{ID: "done", Status: models.ScanCompleted, Branch: "main", ProjectID: "p1"},
		{ID: "older", Status: models.ScanCompleted, Branch: "main", ProjectID: "p1"},
	})
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		h(w, r)
	})
	got, err := scans.LatestCompleted(context.Background(), b, "p1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != "done" {
		t.Fatalf("got %+v, want scan done", got)
	}
	if gotQuery["project-id"][0] != "p1" || gotQuery["branch"][0] != "main" || gotQuery["statuses"][0] != "Completed" {
		t.Fatalf("filters not sent: %v", gotQuery)
	}
}

func TestLatestCompletedNoneReturnsNil(t *testing.T) {
	b := newBackend(t, scansHandler([]models.ScanResponseModel{
		{ID: "run", Status: models.ScanRunning, Branch: "main"},
	}))
	got, err := scans.LatestCompleted(context.Background(), b, "p1", "main")
	if err != nil || got != nil {
		t.Fatalf("got %+v, %v; want nil, nil", got, err)
	}
}

func TestNewFindingsCapsPerEngine(t *testing.T) {
	var newRes, oldRes []*models.ScanResult
	for i := 0; i < 2500; i++ {
		newRes = append(newRes, &models.ScanResult{ID: strconv.Itoa(i), SimilarityID: "n" + strconv.Itoa(i), Severity: "HIGH", Type: "sast"})
	}
	for i := 0; i < 5; i++ { // recurring: present in both
		r := &models.ScanResult{ID: "r" + strconv.Itoa(i), SimilarityID: "rec" + strconv.Itoa(i), Severity: "HIGH", Type: "sast"}
		oldRes = append(oldRes, r)
		newRes = append(newRes, r)
	}
	fake := &fakeResultsServer{t: t, oldID: "old", newID: "new", oldRes: oldRes, newRes: newRes}
	b := newBackend(t, fake.handler())

	got, err := scans.NewFindings(context.Background(), b, "old", "new", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.NewFindings) != 2000 {
		t.Fatalf("NewFindings = %d, want 2000", len(got.NewFindings))
	}
	if !got.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if got.Totals.New != 2500 {
		t.Fatalf("Totals.New = %d, want 2500 (uncapped count)", got.Totals.New)
	}

	small, err := scans.NewFindings(context.Background(), b, "old", "new", 3000)
	if err != nil {
		t.Fatal(err)
	}
	if len(small.NewFindings) != 2500 || small.Truncated {
		t.Fatalf("got %d truncated=%v, want 2500 false", len(small.NewFindings), small.Truncated)
	}
}

func TestLatestCompletedPicksNewestAcrossPagesOldestFirst(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var list []models.ScanResponseModel
	for i := 0; i < 250; i++ { // oldest first; newest Completed is on page 3
		list = append(list, models.ScanResponseModel{
			ID: "s" + strconv.Itoa(i), Status: models.ScanCompleted, CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}
	list = append(list, models.ScanResponseModel{ID: "running-newest", Status: models.ScanRunning, CreatedAt: base.Add(1000 * time.Hour)})
	b := newBackend(t, scansHandler(list))
	got, err := scans.LatestCompleted(context.Background(), b, "p1", "main")
	if err != nil || got == nil || got.ID != "s249" {
		t.Fatalf("got %+v, %v; want s249", got, err)
	}
}

func sastRes(i int, sev string) *models.ScanResult {
	return &models.ScanResult{ID: "r" + strconv.Itoa(i), SimilarityID: "n" + strconv.Itoa(1000+i), Severity: sev, Type: "sast"}
}

func TestNewFindingsKeepsMostSevereDeterministically(t *testing.T) {
	var newRes []*models.ScanResult
	for i := 0; i < 10; i++ {
		newRes = append(newRes, sastRes(i, "LOW"))
	}
	for i := 10; i < 13; i++ {
		newRes = append(newRes, sastRes(i, "CRITICAL"))
	}
	for i := 13; i < 17; i++ {
		newRes = append(newRes, sastRes(i, "HIGH"))
	}
	rand.New(rand.NewSource(7)).Shuffle(len(newRes), func(i, j int) { newRes[i], newRes[j] = newRes[j], newRes[i] })
	fake := &fakeResultsServer{t: t, oldID: "old", newID: "new", newRes: newRes}
	b := newBackend(t, fake.handler())

	ids := func() []string {
		got, err := scans.NewFindings(context.Background(), b, "old", "new", 5)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Truncated || len(got.NewFindings) != 5 {
			t.Fatalf("truncated=%v n=%d", got.Truncated, len(got.NewFindings))
		}
		var out []string
		for _, r := range got.NewFindings {
			out = append(out, r.Severity+":"+r.ID)
		}
		return out
	}
	first := ids()
	want := []string{"CRITICAL:r10", "CRITICAL:r11", "CRITICAL:r12", "HIGH:r13", "HIGH:r14"}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("kept %v, want %v", first, want)
	}
	// Reshuffle the server order; result must be identical.
	rand.New(rand.NewSource(99)).Shuffle(len(fake.newRes), func(i, j int) { fake.newRes[i], fake.newRes[j] = fake.newRes[j], fake.newRes[i] })
	if second := ids(); !reflect.DeepEqual(second, want) {
		t.Fatalf("second call kept %v, want %v", second, want)
	}
}

func TestNewFindingsCountAtCapNotTruncated(t *testing.T) {
	var newRes []*models.ScanResult
	for i := 0; i < 5; i++ {
		newRes = append(newRes, sastRes(i, "HIGH"))
	}
	fake := &fakeResultsServer{t: t, oldID: "old", newID: "new", newRes: newRes}
	got, err := scans.NewFindings(context.Background(), newBackend(t, fake.handler()), "old", "new", 5)
	if err != nil || got.Truncated || len(got.NewFindings) != 5 {
		t.Fatalf("got n=%d truncated=%v err=%v", len(got.NewFindings), got.Truncated, err)
	}
}

func TestNewFindingsCapsEnginesIndependently(t *testing.T) {
	var newRes []*models.ScanResult
	for i := 0; i < 4; i++ {
		newRes = append(newRes, sastRes(i, "HIGH"))
	}
	for i := 0; i < 2; i++ {
		newRes = append(newRes, &models.ScanResult{ID: "c" + strconv.Itoa(i), SimilarityID: "sca" + strconv.Itoa(i), Severity: "HIGH", Type: "sca"})
	}
	fake := &fakeResultsServer{t: t, oldID: "old", newID: "new", newRes: newRes}
	got, err := scans.NewFindings(context.Background(), newBackend(t, fake.handler()), "old", "new", 3)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range got.NewFindings {
		counts[r.Type]++
	}
	if counts["sast"] != 3 || counts["sca"] != 2 || !got.Truncated {
		t.Fatalf("counts=%v truncated=%v", counts, got.Truncated)
	}
}

func TestNewFindingsNonPositiveCapMeansNoCap(t *testing.T) {
	var newRes []*models.ScanResult
	for i := 0; i < 7; i++ {
		newRes = append(newRes, sastRes(i, "HIGH"))
	}
	for _, c := range []int{0, -1} {
		fake := &fakeResultsServer{t: t, oldID: "old", newID: "new", newRes: newRes}
		got, err := scans.NewFindings(context.Background(), newBackend(t, fake.handler()), "old", "new", c)
		if err != nil || got.Truncated || len(got.NewFindings) != 7 {
			t.Fatalf("cap %d: n=%d truncated=%v err=%v", c, len(got.NewFindings), got.Truncated, err)
		}
	}
}
