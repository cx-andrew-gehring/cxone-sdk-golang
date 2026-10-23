//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestAnalyticsVulnerabilitiesBySeverity(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		stats, err := c.Advanced().Analytics().VulnerabilitiesBySeverityTotal(ctx, models.AnalyticsFilter{})
		if err != nil {
			t.Fatalf("analytics severity: %v", err)
		}
		t.Logf("total=%d loc=%d distributions=%d", stats.Total, stats.LOC, len(stats.Distribution))
	})
}

func TestAnalyticsMTTR(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		stats, err := c.Advanced().Analytics().MeanTimeToResolution(ctx, models.AnalyticsFilter{})
		if err != nil {
			t.Fatalf("analytics mttr: %v", err)
		}
		t.Logf("totalResults=%d meanTimeEntries=%d", stats.TotalResults, len(stats.MeanTimeData))
	})
}

func TestAnalyticsIDETotal(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		entries, err := c.Advanced().Analytics().IDETotal(ctx)
		if err != nil {
			t.Fatalf("analytics ide: %v", err)
		}
		t.Logf("IDE entries = %d", len(entries))
	})
}
