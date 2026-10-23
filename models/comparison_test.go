package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// TestScanComparisonJSONRoundTrip pins the wire shape of the typed
// comparison result. The shape doubles as the API contract between the SDK
// and any downstream consumer (e.g. the cxgate web app), so marshalling
// regressions show up immediately.
func TestScanComparisonJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	original := models.ScanComparison{
		OldScanID:   "old-scan",
		NewScanID:   "new-scan",
		GeneratedAt: ts,
		Identity:    string(models.IdentityKeySimilarityID),
		BySeverity: map[string]models.ScanComparisonBucket{
			"HIGH":   {New: 3, Resolved: 1, Recurrent: 5},
			"MEDIUM": {New: 0, Resolved: 0, Recurrent: 0},
		},
		Totals: models.ScanComparisonBucket{New: 3, Resolved: 1, Recurrent: 5},
		NotExploitable: []models.NotExploitableGroup{
			{QueryName: "SQL_Injection", Severity: "HIGH", Count: 2},
			{QueryName: "XSS", Severity: "MEDIUM", Count: 1},
		},
	}
	buf, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got models.ScanComparison
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.OldScanID != original.OldScanID || got.NewScanID != original.NewScanID {
		t.Errorf("scan ids did not round-trip: %+v", got)
	}
	if got.Identity != original.Identity {
		t.Errorf("identity did not round-trip: got %q want %q", got.Identity, original.Identity)
	}
	if !got.GeneratedAt.Equal(ts) {
		t.Errorf("GeneratedAt did not round-trip: %v", got.GeneratedAt)
	}
	if got.BySeverity["HIGH"].New != 3 {
		t.Errorf("HIGH.New = %d, want 3", got.BySeverity["HIGH"].New)
	}
	if got.Totals.New != 3 {
		t.Errorf("Totals.New = %d, want 3", got.Totals.New)
	}
	if len(got.NotExploitable) != 2 || got.NotExploitable[0].QueryName != "SQL_Injection" {
		t.Errorf("NotExploitable did not round-trip: %+v", got.NotExploitable)
	}
}

// TestScanComparisonBucketZeroValueRendersExplicitZeros ensures the counter
// fields render as 0 (not omitted) when empty — the React UI relies on the
// keys being present.
func TestScanComparisonBucketZeroValueRendersExplicitZeros(t *testing.T) {
	bucket := models.ScanComparisonBucket{}
	buf, err := json.Marshal(bucket)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(buf)
	want := `{"new":0,"resolved":0,"recurrent":0}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// TestIdentityKeyConstants pins the public names so downstream code can
// reference them without importing internal/.
func TestIdentityKeyConstants(t *testing.T) {
	if string(models.IdentityKeySimilarityID) != "similarityId" {
		t.Errorf("IdentityKeySimilarityID = %q", models.IdentityKeySimilarityID)
	}
	if string(models.IdentityKeyResultHash) != "resultHash" {
		t.Errorf("IdentityKeyResultHash = %q", models.IdentityKeyResultHash)
	}
}
