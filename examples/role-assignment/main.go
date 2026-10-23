// role-assignment demonstrates the high-level role workflows: listing
// roles via the cached RoleReader, resolving composites, and looking up
// the ast-app client ID for app-role operations.
//
// Usage:
//
//	export CX_TENANT=your-tenant CX_API_KEY=your-key CX_REGION=US
//	go run ./examples/role-assignment
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
)

func main() {
	client, err := cxone.NewClient().
		Region(regionFromEnv()).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("role-assignment-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// --- Resolve the ast-app client ID (cached after first call) ---

	astAppID, err := client.Clients().ASTAppID(ctx)
	if err != nil {
		log.Fatalf("get ast-app id: %v", err)
	}
	fmt.Printf("ast-app client UUID: %s\n\n", astAppID)

	// --- RoleReader: cached realm role catalog ---

	reader := client.Roles().NewReader()

	realmRoles, err := reader.List(ctx)
	if err != nil {
		log.Fatalf("list realm roles: %v", err)
	}
	fmt.Printf("Realm roles (%d):\n", len(realmRoles))
	for _, r := range realmRoles {
		comp := ""
		if r.Composite {
			comp = " [composite]"
		}
		fmt.Printf("  %-30s %s%s\n", r.Name, r.ID, comp)
	}

	// --- Composite resolution: walk the sub-roles of the first composite ---

	for _, r := range realmRoles {
		if !r.Composite {
			continue
		}
		subs, err := reader.Composites(ctx, r.ID)
		if err != nil {
			log.Printf("composites for %s: %v", r.Name, err)
			continue
		}
		fmt.Printf("\nComposites of %q (%d):\n", r.Name, len(subs))
		for _, s := range subs {
			fmt.Printf("  %s  %s\n", s.ID, s.Name)
		}
		break
	}

	// --- App (client-scoped) roles ---

	appRoles, err := reader.ListClientRoles(ctx, astAppID)
	if err != nil {
		log.Fatalf("list app roles: %v", err)
	}
	fmt.Printf("\nApplication roles under ast-app (%d):\n", len(appRoles))
	for _, r := range appRoles {
		fmt.Printf("  %-30s %s\n", r.Name, r.ID)
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
