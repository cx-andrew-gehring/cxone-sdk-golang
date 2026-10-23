// Paginated-list demonstrates iterating over a paginated endpoint using the
// SDK's generic Iterator type. It walks every project visible to the caller
// using both the direct Next(ctx) style and the Go 1.23 range-over-func
// adapter, printing each project's name as it streams through.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
)

func main() {
	client, err := cxone.NewClient().
		Region(cxone.RegionUS).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("paginated-list-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Construct an iterator with 10 items per page. The iterator
	// transparently fetches the next page when the current buffer drains
	// and stops when the platform returns an empty page.
	it := client.Advanced().Projects().Iter(nil, pagination.Config{PageSize: 10})

	// Style 1: range-over-func (Go 1.23+). Clean and concise.
	fmt.Println("=== range-over-func ===")
	count := 0
	for project, err := range it.All(ctx) {
		if err != nil {
			log.Fatalf("iterate: %v", err)
		}
		count++
		fmt.Printf("  %3d. %s  %s\n", count, project.ID[:8], project.Name)
	}
	fmt.Printf("Total: %d projects\n\n", count)

	// Style 2: direct Next(ctx). Useful when you need to break mid-stream
	// or interleave other logic between items.
	//
	// Note: reusing the same iterator after exhaustion always returns
	// (zero, false, nil). Construct a new one to re-walk from the start.
	it2 := client.Advanced().Projects().Iter(nil, pagination.Config{PageSize: 5})
	fmt.Println("=== direct Next(ctx) — first 3 only ===")
	for i := 0; i < 3; i++ {
		project, ok, err := it2.Next(ctx)
		if err != nil {
			log.Fatalf("next: %v", err)
		}
		if !ok {
			fmt.Println("  (fewer than 3 projects)")
			break
		}
		fmt.Printf("  %s  %s\n", project.ID[:8], project.Name)
	}

	// Style 3: Collect everything into a slice. Use sparingly — only for
	// small result sets where holding the full list in memory is acceptable.
	it3 := client.Advanced().Projects().Iter(nil, pagination.Config{PageSize: 100})
	all, err := pagination.Collect(ctx, it3)
	if err != nil {
		log.Fatalf("collect: %v", err)
	}
	fmt.Printf("\nCollected %d projects into a slice.\n", len(all))
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
