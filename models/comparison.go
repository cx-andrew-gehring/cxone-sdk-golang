package models

import "time"

// IdentityKey selects which result field is used as the cross-scan identity
// when diffing two scans into "new", "resolved", and "recurrent" buckets.
//
// SimilarityID is the canonical Checkmarx One identity for SAST findings: it
// survives small refactors that move a vulnerable line within a file and is
// the field the legacy CxSAST SOAP comparison endpoint used. ResultHash is
// stricter — two findings only match if the exact result hash is identical,
// which is useful when the caller wants to detect any change at all.
type IdentityKey string

// Identity key constants.
const (
	IdentityKeySimilarityID IdentityKey = "similarityId"
	IdentityKeyResultHash   IdentityKey = "resultHash"
)

// ScanComparison is the typed result of comparing two Checkmarx One scans.
//
// It bucketizes the differences between the old and new scan by severity:
//
//   - New: findings present in the new scan but not the old.
//   - Resolved: findings present in the old scan but not the new.
//   - Recurrent: findings present in both scans (matched by Identity).
//
// NotExploitable carries the findings in the new scan whose triage state is
// NOT_EXPLOITABLE, grouped by query — useful for QA-gate reports that need
// to show which legitimate-looking findings were suppressed.
type ScanComparison struct {
	OldScanID      string                          `json:"oldScanId"`
	NewScanID      string                          `json:"newScanId"`
	GeneratedAt    time.Time                       `json:"generatedAt"`
	Identity       string                          `json:"identity"`
	BySeverity     map[string]ScanComparisonBucket `json:"bySeverity"`
	Totals         ScanComparisonBucket            `json:"totals"`
	NotExploitable []NotExploitableGroup           `json:"notExploitable"`

	// NewFindings is populated only by the workflow NewFindings call: the
	// findings present in the new scan but not the old, capped per engine
	// (see Truncated). It is empty (and omitted from JSON) for plain
	// comparisons.
	NewFindings []*ScanResult `json:"newFindings,omitempty"`
	// Truncated is true when NewFindings dropped findings because at least
	// one engine exceeded the per-engine cap. Totals.New and BySeverity
	// always reflect the uncapped counts.
	Truncated bool `json:"truncated,omitempty"`
}

// ScanComparisonBucket is the per-severity (or aggregate) count of differences
// between two scans. Fields render as explicit zeros — downstream consumers
// rely on the keys being present even when no findings fall into a given
// bucket.
type ScanComparisonBucket struct {
	New       int `json:"new"`
	Resolved  int `json:"resolved"`
	Recurrent int `json:"recurrent"`
}

// NotExploitableGroup is one row of the "not exploitable" summary table in a
// scan comparison: the count of findings in the new scan that share a query
// name and were marked NOT_EXPLOITABLE by triage.
type NotExploitableGroup struct {
	QueryName string `json:"queryName"`
	Severity  string `json:"severity"`
	Count     int    `json:"count"`
}
