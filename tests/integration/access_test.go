//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
)

func TestAccessibleResources(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := c.Advanced().Access().AccessibleResources(ctx, []string{"project"}, "manage-scan")
		if err != nil {
			t.Fatalf("accessible resources: %v", err)
		}
		t.Logf("all=%v resources=%d", resp.All, len(resp.Resources))
		for _, r := range resp.Resources[:min(3, len(resp.Resources))] {
			t.Logf("  %s (%s) roles=%v", r.ResourceName, r.ResourceType, r.Roles)
		}
	})
}

func TestEntitiesForProject(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Find a project to inspect access for.
		projects, err := c.Advanced().Projects().List(ctx, nil)
		if err != nil || len(projects.Projects) == 0 {
			t.Skip("no projects available")
		}
		pid := projects.Projects[0].ID

		entities, err := c.Advanced().Access().EntitiesFor(ctx, pid, "project")
		if err != nil {
			t.Fatalf("entities for project %s: %v", pid, err)
		}
		t.Logf("project %s has %d access assignments", pid, len(entities))
	})
}
