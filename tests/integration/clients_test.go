//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestListClients(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		clients, err := c.Advanced().Clients().List(ctx, models.OIDCClientFilter{Max: 10})
		if err != nil {
			t.Fatalf("list clients: %v", err)
		}
		t.Logf("fetched %d clients (max=10)", len(clients))
		if len(clients) == 0 {
			t.Skip("no OIDC clients on this tenant")
		}
		for _, cl := range clients[:min(3, len(clients))] {
			t.Logf("  %s (%s)", cl.ClientID, cl.ID)
		}
	})
}

func TestASTAppIDCache(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		id, err := c.Clients().ASTAppID(ctx)
		if err != nil {
			t.Fatalf("ast-app id: %v", err)
		}
		if id == "" {
			t.Fatal("ast-app id is empty")
		}
		t.Logf("ast-app UUID = %s", id)

		// Second call should be cached.
		id2, err := c.Clients().ASTAppID(ctx)
		if err != nil {
			t.Fatalf("ast-app id (cached): %v", err)
		}
		if id2 != id {
			t.Errorf("cache miss: %q != %q", id2, id)
		}
	})
}

func TestListClientScopes(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		scopes, err := c.Advanced().Clients().ListScopes(ctx)
		if err != nil {
			t.Fatalf("list scopes: %v", err)
		}
		t.Logf("client scopes = %d", len(scopes))
		for _, s := range scopes[:min(5, len(scopes))] {
			t.Logf("  %s (%s)", s.Name, s.Protocol)
		}
	})
}
