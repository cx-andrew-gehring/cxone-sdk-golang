package projects

import "time"

// SetConflictLookupBackoffForTest replaces the re-lookup delays used after a
// duplicate-name create (tests only).
func SetConflictLookupBackoffForTest(d []time.Duration) { conflictLookupBackoff = d }
