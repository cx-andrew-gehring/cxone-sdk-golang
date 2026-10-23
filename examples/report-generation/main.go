// Report-generation demonstrates requesting a JSON report, polling until
// it's ready, and downloading the result. Uses the high-level reports
// workflow builder + WaitAndDownload convenience.
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/reports"
)

func main() {
	client, err := cxone.NewClient().
		Region(cxone.RegionUS).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("report-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Queue a JSON report.
	h, err := client.Reports().NewJSONReport().
		ForScan(mustEnv("CX_SCAN_ID"), mustEnv("CX_PROJECT_ID"), "main").
		WithSections("scan-summary", "results").
		WithScanners("sast", "sca").
		Generate(ctx)
	if err != nil {
		log.Fatalf("generate report: %v", err)
	}
	fmt.Printf("Report %s queued\n", h.ReportID())

	// Poll until ready + download.
	var buf bytes.Buffer
	n, err := h.WaitAndDownload(ctx, &buf, reports.WaitOptions{
		PollInterval: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("download report: %v", err)
	}
	fmt.Printf("Downloaded %d bytes (URL: %s)\n", n, h.URL())

	// Write to stdout or a file for inspection.
	if outPath := os.Getenv("CX_REPORT_OUT"); outPath != "" {
		if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
			log.Fatalf("write file: %v", err)
		}
		fmt.Printf("Saved to %s\n", outPath)
	} else {
		// Truncate for display.
		preview := buf.String()
		if len(preview) > 500 {
			preview = preview[:500] + "...\n"
		}
		fmt.Println(preview)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
