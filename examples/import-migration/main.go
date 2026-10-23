// import-migration demonstrates the migration/import workflow: starting an
// import from a previously-uploaded archive and polling until completion
// using the ImportInspector + WaitUntilComplete pattern.
//
// This example requires a pre-uploaded data archive. Use the uploads
// endpoint to upload one first, then pass the filename here.
//
// Usage:
//
//	export CX_TENANT=your-tenant CX_API_KEY=your-key CX_REGION=US
//	export CX_IMPORT_FILE=data-archive-filename.zip
//	export CX_IMPORT_KEY=encryption-key-used-during-export
//	go run ./examples/import-migration
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/migration"
)

func main() {
	client, err := cxone.NewClient().
		Region(regionFromEnv()).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("import-migration-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// --- List existing imports ---

	imports, err := client.Advanced().Migration().List(ctx)
	if err != nil {
		log.Fatalf("list imports: %v", err)
	}
	fmt.Printf("Existing imports: %d\n", len(imports))
	for _, imp := range imports {
		fmt.Printf("  %s  status=%s\n", imp.MigrationID, imp.Status)
	}

	// --- Start a new import (if env vars are set) ---

	fileName := os.Getenv("CX_IMPORT_FILE")
	encryptionKey := os.Getenv("CX_IMPORT_KEY")
	if fileName == "" || encryptionKey == "" {
		fmt.Println("\nSet CX_IMPORT_FILE and CX_IMPORT_KEY to start a new import (skipped)")
		return
	}

	migrationID, err := client.Advanced().Migration().StartImport(ctx, fileName, "", encryptionKey)
	if err != nil {
		log.Fatalf("start import: %v", err)
	}
	fmt.Printf("\nStarted import: migrationID=%s\n", migrationID)

	// --- Poll until complete using the workflow inspector ---

	insp, err := client.Migration().GetImport(ctx, migrationID)
	if err != nil {
		log.Fatalf("get import: %v", err)
	}

	fmt.Printf("Initial status: %s\n", insp.Status())
	fmt.Println("Polling...")

	final, err := migration.WaitUntilComplete(ctx, insp, migration.WaitOptions{
		PollInterval: 10 * time.Second,
		MaxDuration:  10 * time.Minute,
	})
	if err != nil {
		log.Fatalf("import failed: %v", err)
	}

	fmt.Printf("Final status: %s\n", final.Status())
	if final.Data() != nil && len(final.Data().Logs) > 0 {
		fmt.Println("Logs:")
		for _, entry := range final.Data().Logs {
			fmt.Printf("  [%s] %s\n", entry.Level, entry.Message)
		}
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
