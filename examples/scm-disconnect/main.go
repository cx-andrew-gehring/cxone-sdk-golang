package main

import (
	"context"
	"fmt"
	"log"
	"os"

	cxone "github.com/checkmarx-open-labs/cxone-sdk-golang"
)

func main() {
	client, err := cxone.NewClient().
		Region(cxone.RegionIndia).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("scm-disconnect-test").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	projectID := mustEnv("CX_PROJECT_ID")

	if err := client.Advanced().SCM().DisconnectProject(ctx, projectID); err != nil {
		log.Fatalf("disconnect: %v", err)
	}
	fmt.Printf("Disconnected project %s (or already manual)\n", projectID)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
