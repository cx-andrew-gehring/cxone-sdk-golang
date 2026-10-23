//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestListSASTPresets(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := c.Advanced().PresetManager().ListPresets(ctx, "sast", 10)
		if err != nil {
			t.Fatalf("list SAST presets: %v", err)
		}
		t.Logf("SAST presets = %d (total=%d)", len(resp.Presets), resp.TotalCount)
		for _, p := range resp.Presets[:min(3, len(resp.Presets))] {
			t.Logf("  %s (%s) custom=%v", p.Name, p.ID, p.Custom)
		}
	})
}

func TestGetSASTPresetByID(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := c.Advanced().PresetManager().ListPresets(ctx, "sast", 1)
		if err != nil || len(resp.Presets) == 0 {
			t.Skip("no SAST presets available")
		}
		id := resp.Presets[0].ID

		preset, err := c.Advanced().PresetManager().GetPreset(ctx, "sast", id)
		if err != nil {
			t.Fatalf("get preset %s: %v", id, err)
		}
		t.Logf("preset=%s families=%d", preset.Name, len(preset.QueryFamilies))
	})
}

func TestListQueryFamilies(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		families, err := c.Advanced().PresetManager().ListQueryFamilies(ctx, "sast")
		if err != nil {
			t.Fatalf("list query families: %v", err)
		}
		t.Logf("SAST query families = %d", len(families))
		for _, f := range families[:min(5, len(families))] {
			t.Logf("  %s", f)
		}
	})
}

func TestCreateAndDeletePreset(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		name := "sdk-test-preset-" + t.Name()
		created, err := c.Advanced().PresetManager().CreatePreset(ctx, "sast", &models.PresetCreateRequest{
			Name: name,
		})
		if err != nil {
			t.Fatalf("create preset: %v", err)
		}
		t.Logf("created preset %s (%s)", created.Name, created.ID)

		t.Cleanup(func() {
			dctx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer dcancel()
			if err := c.Advanced().PresetManager().DeletePreset(dctx, "sast", created.ID); err != nil {
				t.Errorf("cleanup delete: %v", err)
			}
		})

		if created.Name != name {
			t.Errorf("name = %q, want %q", created.Name, name)
		}
	})
}

func TestAuditSessionLifecycle(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		projectID := os.Getenv("TEST_PROJECT_ID")
		scanID := os.Getenv("TEST_SCAN_ID")
		if projectID == "" || scanID == "" {
			t.Skip("TEST_PROJECT_ID and TEST_SCAN_ID required for audit session tests")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		sess, err := c.Advanced().Audit().CreateSession(ctx, &models.AuditCreateRequest{
			Scanner:   "sast",
			ProjectID: projectID,
			ScanID:    scanID,
		})
		if err != nil {
			t.Fatalf("create audit session: %v", err)
		}
		t.Logf("session=%s status=%s", sess.ID, sess.Data.Status)

		t.Cleanup(func() {
			dctx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer dcancel()
			_ = c.Advanced().Audit().DeleteSession(dctx, sess.ID)
		})

		// Keep alive
		if err := c.Advanced().Audit().KeepAlive(ctx, sess.ID); err != nil {
			t.Fatalf("keep alive: %v", err)
		}

		// Get query tree
		tree, err := c.Advanced().Audit().GetQueryTree(ctx, sess.ID, "", "")
		if err != nil {
			t.Logf("get query tree: %v (may require session to be fully initialized)", err)
		} else {
			t.Logf("query tree nodes = %d", len(tree))
		}
	})
}
