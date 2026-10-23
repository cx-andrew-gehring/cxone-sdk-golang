//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestListUsers(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		users, err := c.Advanced().Users().List(ctx, models.UserFilter{Max: 10})
		if err != nil {
			t.Fatalf("list users: %v", err)
		}
		t.Logf("fetched %d users (max=10)", len(users))
		if len(users) == 0 {
			t.Skip("no users on this tenant")
		}
		if users[0].ID == "" {
			t.Error("first user has empty ID")
		}
	})
}

func TestUserCount(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		count, err := c.Advanced().Users().Count(ctx, models.UserFilter{})
		if err != nil {
			t.Fatalf("user count: %v", err)
		}
		t.Logf("user count = %d", count)
	})
}

func TestUserGetByID(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		users, err := c.Advanced().Users().List(ctx, models.UserFilter{Max: 1})
		if err != nil {
			t.Fatalf("list users: %v", err)
		}
		if len(users) == 0 {
			t.Skip("no users on this tenant")
		}

		user, err := c.Advanced().Users().Get(ctx, users[0].ID)
		if err != nil {
			t.Fatalf("get user by id: %v", err)
		}
		if user.ID != users[0].ID {
			t.Errorf("ID mismatch: %q vs %q", user.ID, users[0].ID)
		}
		t.Logf("user=%s (%s)", user.Username, user.ID)
	})
}

func TestUserReaderCache(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reader := c.Users().NewReader()
		all, err := reader.List(ctx)
		if err != nil {
			t.Fatalf("reader list: %v", err)
		}
		t.Logf("reader cached %d users", len(all))

		if len(all) > 0 {
			u, err := reader.ByID(ctx, all[0].ID)
			if err != nil {
				t.Fatalf("reader byID: %v", err)
			}
			if u == nil {
				t.Error("ByID returned nil for known user")
			}
		}
	})
}

func TestUserGroups(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		users, err := c.Advanced().Users().List(ctx, models.UserFilter{Max: 1})
		if err != nil || len(users) == 0 {
			t.Skip("no users available")
		}

		groups, err := c.Advanced().Users().GetGroups(ctx, users[0].ID)
		if err != nil {
			t.Fatalf("get user groups: %v", err)
		}
		t.Logf("user %s is in %d groups", users[0].Username, len(groups))
	})
}
