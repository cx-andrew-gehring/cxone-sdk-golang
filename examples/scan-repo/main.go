// Scan-repo demonstrates the high-level workflow: create (or reuse) a
// project, start a SAST+SCA scan against a Git repository, wait for
// completion, and print the final state. Uses the fluent ScanInvoker
// builder and the WaitUntilComplete waiter.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	cxone "github.com/checkmarx-open-labs/cxone-sdk-golang"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scans"
)

func main() {
	client, err := cxone.NewClient().
		Region(cxone.RegionUS).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("scan-repo-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Load the project. If it doesn't exist yet, create it via the
	// low-level Advanced handle and wrap the response.
	projectID := os.Getenv("CX_PROJECT_ID")
	if projectID == "" {
		p, err := client.Advanced().Projects().Create(ctx, &models.Project{
			Name:       "scan-repo-example",
			MainBranch: "main",
			RepoURL:    mustEnv("CX_REPO_URL"),
		})
		if err != nil {
			log.Fatalf("create project: %v", err)
		}
		projectID = p.ID
		fmt.Printf("Created project %s (%s)\n", p.Name, p.ID)
	}

	repo, err := client.Projects().Get(ctx, projectID)
	if err != nil {
		log.Fatalf("load project: %v", err)
	}

	// Start a SAST+SCA scan on the main branch.
	insp, err := client.Scans().NewScan(repo).
		ForBranch("main").
		WithEngines("sast", "sca").
		// Scan-level tags (does not touch the project's tags; use
		// WithProjectTags for that).
		WithTags(map[string]string{"source": "sdk-example"}).
		Start(ctx)
	if err != nil {
		log.Fatalf("start scan: %v", err)
	}
	fmt.Printf("Scan %s started (status=%s)\n", insp.ID(), insp.Status())

	// Poll until the scan reaches a terminal state.
	final, err := scans.WaitUntilComplete(ctx, insp, scans.WaitOptions{
		PollInterval: 15 * time.Second,
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
