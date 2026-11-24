package scans_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

func scansHandler(list []models.ScanResponseModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		off, _ := strconv.Atoi(q.Get("offset"))
		out := list
		if off > 0 {
			out = nil
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
	if gotQuery["sort"][0] != "-created_at" {
		t.Fatalf("sort = %v", gotQuery["sort"])
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
