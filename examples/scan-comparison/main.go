// Scan-comparison demonstrates diffing two completed scans into typed
// new/resolved/recurrent buckets by severity, plus the not-exploitable
// summary used by QA-gate reports.
//
// Usage:
//
//	export CX_TENANT=...
//	export CX_API_KEY=...    # or CX_OAUTH_CLIENT_ID + CX_OAUTH_CLIENT_SECRET
//	export CX_REGION=US      # or US2, EU, EU2, DEU, ANZ, India, Singapore, UAE
//	export CX_OLD_SCAN_ID=<baseline scan id>
//	export CX_NEW_SCAN_ID=<candidate scan id>
//	go run ./examples/scan-comparison
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
)

func main() {
	region := regionFromEnv()

	builder := cxone.NewClient().
		Region(region).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("scan-comparison-example")

	switch {
	case os.Getenv("CX_OAUTH_CLIENT_ID") != "" && os.Getenv("CX_OAUTH_CLIENT_SECRET") != "":
		builder = builder.OAuth(os.Getenv("CX_OAUTH_CLIENT_ID"), os.Getenv("CX_OAUTH_CLIENT_SECRET"))
	case os.Getenv("CX_API_KEY") != "":
		builder = builder.APIKey(os.Getenv("CX_API_KEY"))
	default:
		log.Fatal("either CX_OAUTH_CLIENT_ID+CX_OAUTH_CLIENT_SECRET or CX_API_KEY must be set")
	}

	client, err := builder.Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	oldID := mustEnv("CX_OLD_SCAN_ID")
	newID := mustEnv("CX_NEW_SCAN_ID")

	cmp, err := client.Scans().Compare(ctx, oldID, newID)
	if err != nil {
		log.Fatalf("compare scans: %v", err)
	}

	fmt.Printf("Comparison %s → %s  (identity=%s, generated=%s)\n",
		cmp.OldScanID, cmp.NewScanID, cmp.Identity, cmp.GeneratedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Printf("Totals:  new=%d  resolved=%d  recurrent=%d\n",
		cmp.Totals.New, cmp.Totals.Resolved, cmp.Totals.Recurrent)
	fmt.Println("By severity:")
	for _, sev := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO"} {
		b, ok := cmp.BySeverity[sev]
		if !ok {
			continue
		}
		fmt.Printf("  %-9s  new=%-3d  resolved=%-3d  recurrent=%-3d\n", sev, b.New, b.Resolved, b.Recurrent)
	}
	if len(cmp.NotExploitable) > 0 {
		fmt.Println("Not exploitable (new scan), by query:")
		for _, g := range cmp.NotExploitable {
			fmt.Printf("  %-30s  severity=%-7s  count=%d\n", g.QueryName, g.Severity, g.Count)
		}
	}
}

// regionFromEnv maps CX_REGION to one of the well-known constants.
func regionFromEnv() cxone.Region {
	switch os.Getenv("CX_REGION") {
	case "US2":
		return cxone.RegionUS2
	case "EU":
		return cxone.RegionEU
	case "EU2":
		return cxone.RegionEU2
	case "DEU":
		return cxone.RegionDEU
	case "ANZ":
		return cxone.RegionANZ
	case "India":
		return cxone.RegionIndia
	case "Singapore":
		return cxone.RegionSingapore
	case "UAE":
		return cxone.RegionUAE
	default:
		return cxone.RegionUS
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
