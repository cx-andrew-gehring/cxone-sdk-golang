//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestListAdminGroups(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		groups, err := c.Advanced().Groups().ListAdmin(ctx, models.GroupFilter{Max: 10})
		if err != nil {
			t.Fatalf("list admin groups: %v", err)
		}
		t.Logf("admin groups = %d (max=10)", len(groups))
		for _, g := range groups[:min(3, len(groups))] {
			t.Logf("  %s (%s) path=%s subGroups=%d", g.Name, g.ID, g.Path, g.SubGroupCount)
		}
	})
}

func TestGroupCount(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		count, err := c.Advanced().Groups().Count(ctx, models.GroupFilter{TopLevel: true})
		if err != nil {
			t.Fatalf("group count: %v", err)
		}
		t.Logf("top-level group count = %d", count)
	})
}

func TestGroupGetByID(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		groups, err := c.Advanced().Groups().ListAdmin(ctx, models.GroupFilter{Max: 1})
		if err != nil || len(groups) == 0 {
			t.Skip("no groups available")
		}

		g, err := c.Advanced().Groups().GetByID(ctx, groups[0].ID)
		if err != nil {
			t.Fatalf("get group by id: %v", err)
		}
		if g.ID != groups[0].ID {
			t.Errorf("ID mismatch: %q vs %q", g.ID, groups[0].ID)
		}
		t.Logf("group=%s realmRoles=%v subGroupCount=%d", g.Name, g.RealmRoles, g.SubGroupCount)
	})
}

func TestGroupChildren(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		groups, err := c.Advanced().Groups().ListAdmin(ctx, models.GroupFilter{Max: 20})
		if err != nil {
			t.Fatalf("list groups: %v", err)
		}
		for _, g := range groups {
			if g.SubGroupCount == 0 {
				continue
			}
			children, err := c.Advanced().Groups().GetChildren(ctx, g.ID, 0, 10)
			if err != nil {
				t.Fatalf("get children of %s: %v", g.Name, err)
			}
			t.Logf("group %q has %d children (fetched %d)", g.Name, g.SubGroupCount, len(children))
			return
		}
		t.Skip("no groups with children found")
	})
}
