// analytics-dashboard demonstrates the analytics KPI endpoints: fetching
// vulnerability distribution by severity, mean time to resolution, and
// most-common vulnerabilities.
//
// Usage:
//
//	export CX_TENANT=your-tenant CX_API_KEY=your-key CX_REGION=US
//	go run ./examples/analytics-dashboard
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func main() {
	client, err := cxone.NewClient().
		Region(regionFromEnv()).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("analytics-dashboard-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	analytics := client.Advanced().Analytics()

	// --- Vulnerabilities by severity ---

	severity, err := analytics.VulnerabilitiesBySeverityTotal(ctx, models.AnalyticsFilter{})
	if err != nil {
		log.Fatalf("severity KPI: %v", err)
	}
	fmt.Printf("Vulnerabilities by Severity (total=%d, LOC=%d)\n", severity.Total, severity.LOC)
	for _, block := range severity.Distribution {
		fmt.Printf("  %s:\n", block.Label)
		for _, v := range block.Values {
			fmt.Printf("    %-12s %d results (%.1f%%)\n", v.Label, v.Results, v.Percentage)
		}
	}

	// --- Mean time to resolution ---

	mttr, err := analytics.MeanTimeToResolution(ctx, models.AnalyticsFilter{})
	if err != nil {
		log.Fatalf("MTTR KPI: %v", err)
	}
	fmt.Printf("\nMean Time to Resolution (totalResults=%d)\n", mttr.TotalResults)
	for _, entry := range mttr.MeanTimeData {
		fmt.Printf("  %-12s %d results, meanTime=%ds\n", entry.Label, entry.Results, entry.MeanTime)
	}

	// --- Most common vulnerabilities (top 5) ---

	common, err := analytics.MostCommonVulnerabilities(ctx, 5, models.AnalyticsFilter{})
	if err != nil {
		log.Fatalf("most-common KPI: %v", err)
	}
	fmt.Printf("\nMost Common Vulnerabilities (top %d)\n", len(common))
	for _, v := range common {
		fmt.Printf("  %-40s total=%d\n", v.VulnerabilityName, v.Total)
	}
}

func regionFromEnv() cxone.Region {
	switch os.Getenv("CX_REGION") {
	case "US":
		return cxone.RegionUS
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
