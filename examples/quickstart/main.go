// Quickstart demonstrates the minimal steps to construct a Checkmarx One
// SDK client and list the first page of projects. It reads credentials
// from environment variables and prints each project's name and id.
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

	client, err := cxone.NewClient().
		Region(region).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("quickstart-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// List the first page of projects using the low-level Advanced handle.
	projects, err := client.Advanced().Projects().List(ctx, nil)
	if err != nil {
		log.Fatalf("list projects: %v", err)
	}

	fmt.Printf("Total projects: %d\n", projects.TotalCount)
	for _, p := range projects.Projects {
		fmt.Printf("  %s  %s\n", p.ID, p.Name)
	}
}

// regionFromEnv maps CX_REGION to one of the well-known constants.
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
