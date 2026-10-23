// Scan-upload demonstrates uploading a local zip file and scanning it.
// It uses the Invoker's upload mode: the SDK obtains a presigned URL,
// PUTs the file to the object store (bypassing the funnel), and references
// the URL when creating the scan.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

func main() {
	client, err := cxone.NewClient().
		Region(cxone.RegionUS).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("scan-upload-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Load the target project.
	repo, err := client.Projects().Get(ctx, mustEnv("CX_PROJECT_ID"))
	if err != nil {
		log.Fatalf("load project: %v", err)
	}

	// Open the zip file.
	zipPath := mustEnv("CX_SOURCE_ZIP")
	f, err := os.Open(zipPath)
	if err != nil {
		log.Fatalf("open zip: %v", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		log.Fatalf("stat zip: %v", err)
	}

	// Start the scan in upload mode.
	insp, err := client.Scans().NewScan(repo).
		WithEngines("sast").
		WithUploadReader(f, stat.Size()).
		Start(ctx)
	if err != nil {
		log.Fatalf("start scan: %v", err)
	}
	fmt.Printf("Scan %s started (upload mode)\n", insp.ID())

	// Wait for completion.
	final, err := scans.WaitUntilComplete(ctx, insp, scans.WaitOptions{
		PollInterval: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("scan failed: %v", err)
	}
	fmt.Printf("Scan %s finished: %s\n", final.ID(), final.StateMessage())
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
