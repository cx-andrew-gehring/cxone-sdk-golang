//go:build integration

package integration

import (
	"context"
	"net/url"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestListApplications(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := c.Advanced().Applications().List(ctx, url.Values{"limit": {"10"}})
		if err != nil {
			t.Fatalf("list applications: %v", err)
		}
		t.Logf("applications = %d (total=%d)", len(resp.Applications), resp.TotalCount)
	})
}

func TestApplicationCount(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		count, err := c.Advanced().Applications().Count(ctx, nil)
		if err != nil {
			t.Fatalf("application count: %v", err)
		}
		t.Logf("application count = %d", count)
	})
}

func TestCreateAndDeleteApplication(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		name := "sdk-integration-test-app-" + t.Name()
		app, err := c.Advanced().Applications().Create(ctx, &models.ApplicationCreateRequest{
			Name: name,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Logf("created application %s (%s)", app.Name, app.ID)

		t.Cleanup(func() {
			dctx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer dcancel()
			if err := c.Advanced().Applications().Delete(dctx, app.ID); err != nil {
				t.Errorf("cleanup delete: %v", err)
			}
		})

		if app.Name != name {
			t.Errorf("name = %q, want %q", app.Name, name)
		}
	})
}
