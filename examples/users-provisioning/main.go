// users-provisioning demonstrates the high-level user workflows: listing
// users via the cached UserReader, looking up a user by email, and
// creating a user if one doesn't already exist (GetOrCreate pattern).
//
// Usage:
//
//	export CX_TENANT=your-tenant CX_API_KEY=your-key CX_REGION=US
//	go run ./examples/users-provisioning
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
		AgentName("users-provisioning-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// --- UserReader: cached listing ---

	reader := client.Users().NewReader()

	count, err := reader.Count(ctx)
	if err != nil {
		log.Fatalf("count users: %v", err)
	}
	fmt.Printf("Total users on tenant: %d\n", count)

	all, err := reader.List(ctx)
	if err != nil {
		log.Fatalf("list users: %v", err)
	}
	fmt.Printf("Fetched %d users into cache\n", len(all))

	// Lookups hit the cache — no extra network calls.
	if len(all) > 0 {
		first := all[0]
		u, _ := reader.ByID(ctx, first.ID)
		fmt.Printf("  ByID(%s) = %s\n", first.ID, u.Username)

		if first.Email != "" {
			u, _ = reader.ByEmail(ctx, first.Email)
			fmt.Printf("  ByEmail(%s) = %s\n", first.Email, u.Username)
		}
	}

	// --- GetOrCreate: idempotent user provisioning ---

	email := os.Getenv("CX_EXAMPLE_EMAIL")
	if email == "" {
		fmt.Println("\nSet CX_EXAMPLE_EMAIL to test GetOrCreateByEmail (skipped)")
		return
	}

	user, err := client.Users().GetOrCreateByEmail(ctx, &models.User{
		Email:     email,
		Username:  email,
		FirstName: "SDK",
		LastName:  "Example",
		Enabled:   true,
	})
	if err != nil {
		log.Fatalf("get-or-create user: %v", err)
	}
	fmt.Printf("\nGetOrCreateByEmail: id=%s username=%s\n", user.ID, user.Username)
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
