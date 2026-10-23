//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestListPolicies(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := c.Advanced().Policies().List(ctx, models.PolicyFilter{Limit: 10, Page: 1})
		if err != nil {
			t.Fatalf("list policies: %v", err)
		}
		t.Logf("policies = %d (filteredCount=%d)", len(resp.Policies), resp.FilteredTotalCount)
		for _, p := range resp.Policies[:min(3, len(resp.Policies))] {
			t.Logf("  %s (%s) active=%v", p.Name, p.ID, p.IsActivated)
		}
	})
}

func TestPolicyCount(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		count, err := c.Advanced().Policies().Count(ctx, models.PolicyFilter{})
		if err != nil {
			t.Fatalf("policy count: %v", err)
		}
		t.Logf("policy count = %d", count)
	})
}

func TestListPolicyViolations(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := c.Advanced().Policies().ListViolations(ctx, models.PolicyViolationFilter{Limit: 5})
		if err != nil {
			t.Fatalf("list violations: %v", err)
		}
		t.Logf("violations = %d (filteredCount=%d)", len(resp.Incidents), resp.FilteredTotalCount)
	})
}
