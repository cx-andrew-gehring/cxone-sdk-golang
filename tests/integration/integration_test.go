//go:build integration

// Package integration contains end-to-end integration tests that run
// against a real Checkmarx One environment.
//
// Every test is executed against BOTH authentication flows (OAuth
// client-credentials and API key) in parallel (CLAUDE.md §15). When
// credentials are absent the test suite is skipped gracefully — it never
// hard-fails in CI without them.
//
// Required environment variables:
//
//	TEST_OAUTH_CLIENT_ID      OAuth client id
//	TEST_OAUTH_CLIENT_SECRET  OAuth client secret
//	TEST_API_KEY              API key (refresh token)
//	TEST_TENANT               Tenant name (IAM realm)
//	TEST_REGION               Region key (US, EU, EU2, …)
//
// Optional:
//
//	TEST_PROJECT_ID           Existing project to scan/inspect
//
// Run:
//
//	go test -tags=integration ./tests/integration/... -timeout 10m
package integration

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// env helpers ----------------------------------------------------------------

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skipping: %s not set", key)
	}
	return v
}

func regionFromKey(key string) cxone.Region {
	switch strings.ToUpper(key) {
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
	case "INDIA":
		return cxone.RegionIndia
	case "SINGAPORE":
		return cxone.RegionSingapore
	case "UAE":
		return cxone.RegionUAE
	default:
		return cxone.RegionUS
	}
}

// dualAuth runs fn against both auth modes in parallel. Each invocation
// gets a fresh *cxone.Client; the test name suffix makes it clear which
// mode is under test.
func dualAuth(t *testing.T, fn func(t *testing.T, c *cxone.Client)) {
	t.Helper()

	tenant := envOrSkip(t, "TEST_TENANT")
	region := regionFromKey(envOrSkip(t, "TEST_REGION"))

	oauthID := os.Getenv("TEST_OAUTH_CLIENT_ID")
	oauthSecret := os.Getenv("TEST_OAUTH_CLIENT_SECRET")
	apiKey := os.Getenv("TEST_API_KEY")

	if oauthID == "" && apiKey == "" {
		t.Skip("skipping: neither TEST_OAUTH_CLIENT_ID nor TEST_API_KEY is set")
	}

	type authCase struct {
		name    string
		builder func() (*cxone.Client, error)
	}
	cases := make([]authCase, 0, 2)

	if oauthID != "" && oauthSecret != "" {
		cases = append(cases, authCase{
			name: "oauth",
			builder: func() (*cxone.Client, error) {
				return cxone.NewClient().
					Region(region).
					Tenant(tenant).
					AgentName("integration-test").
					OAuth(oauthID, oauthSecret).
					Build()
			},
		})
	}
	if apiKey != "" {
		cases = append(cases, authCase{
			name: "apikey",
			builder: func() (*cxone.Client, error) {
				return cxone.NewClient().
					Region(region).
					Tenant(tenant).
					AgentName("integration-test").
					APIKey(apiKey).
					Build()
			},
		})
	}

	var wg sync.WaitGroup
	for _, tc := range cases {
		wg.Add(1)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer wg.Done()
			c, err := tc.builder()
			if err != nil {
				t.Fatalf("build client (%s): %v", tc.name, err)
			}
			t.Cleanup(func() { c.Close() })
			fn(t, c)
		})
	}
	wg.Wait()
}

// tests -----------------------------------------------------------------------

func TestListProjects(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		projects, err := c.Advanced().Projects().List(ctx, nil)
		if err != nil {
			t.Fatalf("list projects: %v", err)
		}
		t.Logf("totalCount=%d (fetched %d)", projects.TotalCount, len(projects.Projects))
	})
}

func TestGetProjectAndBranches(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		projectID := os.Getenv("TEST_PROJECT_ID")
		if projectID == "" {
			t.Skip("TEST_PROJECT_ID not set")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		repo, err := c.Projects().Get(ctx, projectID)
		if err != nil {
			t.Fatalf("get project: %v", err)
		}
		t.Logf("project=%s mainBranch=%s", repo.Name(), repo.MainBranch())

		branches, err := repo.Branches(ctx)
		if err != nil {
			t.Fatalf("branches: %v", err)
		}
		t.Logf("branches=%v", branches)
	})
}

func TestPresetsCache(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		pr := c.Presets()
		all, err := pr.List(ctx)
		if err != nil {
			t.Fatalf("list presets: %v", err)
		}
		t.Logf("presets=%d", len(all))
		if len(all) == 0 {
			t.Skip("no presets in this tenant")
		}

		// Level-2: fetch detail for the first preset.
		detail, err := all[0].Detail(ctx)
		if err != nil {
			t.Fatalf("preset detail: %v", err)
		}
		t.Logf("preset[0]=%s queryIds=%d", all[0].Name(), len(detail.QueryIDs))
	})
}

func TestCreateAndDeleteProject(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		name := "sdk-integration-test-" + t.Name()
		created, err := c.Advanced().Projects().Create(ctx, &models.Project{
			Name: name,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Logf("created project %s (%s)", created.Name, created.ID)

		// Cleanup: always delete, even on failure.
		t.Cleanup(func() {
			dctx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer dcancel()
			if err := c.Advanced().Projects().Delete(dctx, created.ID); err != nil {
				t.Errorf("cleanup delete: %v", err)
			}
		})

		// Verify the project exists.
		repo, err := c.Projects().Get(ctx, created.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if repo.Name() != name {
			t.Errorf("name=%q, want %q", repo.Name(), name)
		}
	})
}

func TestTenantConfiguration(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		entries, err := c.Advanced().Tenant().GetConfiguration(ctx)
		if err != nil {
			t.Fatalf("tenant config: %v", err)
		}
		t.Logf("tenant config entries=%d", len(entries))
		for _, e := range entries {
			t.Logf("  %s = %s", e.Key, e.Value)
		}
	})
}

func TestFeatureFlags(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		flags, err := c.Advanced().FeatureFlags().List(ctx, "")
		if err != nil {
			t.Fatalf("feature flags: %v", err)
		}
		t.Logf("feature flags=%d", len(flags))
	})
}
