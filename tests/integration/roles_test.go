//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
)

func TestListRealmRoles(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		roles, err := c.Advanced().Roles().ListRealm(ctx)
		if err != nil {
			t.Fatalf("list realm roles: %v", err)
		}
		t.Logf("realm roles = %d", len(roles))
		if len(roles) == 0 {
			t.Skip("no realm roles on this tenant")
		}
		for _, r := range roles[:min(3, len(roles))] {
			t.Logf("  %s (composite=%v)", r.Name, r.Composite)
		}
	})
}

func TestRoleReaderComposites(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reader := c.Roles().NewReader()
		all, err := reader.List(ctx)
		if err != nil {
			t.Fatalf("reader list: %v", err)
		}
		t.Logf("cached %d realm roles", len(all))

		for _, r := range all {
			if !r.Composite {
				continue
			}
			subs, err := reader.Composites(ctx, r.ID)
			if err != nil {
				t.Fatalf("composites for %s: %v", r.Name, err)
			}
			t.Logf("composite %q has %d sub-roles", r.Name, len(subs))
			break
		}
	})
}

func TestListClientRoles(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		astAppID, err := c.Clients().ASTAppID(ctx)
		if err != nil {
			t.Fatalf("ast-app id: %v", err)
		}

		roles, err := c.Advanced().Roles().ListClient(ctx, astAppID)
		if err != nil {
			t.Fatalf("list client roles: %v", err)
		}
		t.Logf("ast-app roles = %d", len(roles))
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
