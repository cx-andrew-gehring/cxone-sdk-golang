// Package policies provides the policy-verdict workflow: fetching the
// evaluation result (including the break-build verdict) for a scan.
package policies

import (
	"context"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/policies"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Backend bundles the dependencies the workflow needs to call the
// underlying endpoint package.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}

// Evaluate returns the policy evaluation result for scanID. Check
// BreakBuild on the result for the gating verdict. A scan with no policies
// assigned yields BreakBuild == false and a nil error.
func Evaluate(ctx context.Context, b *Backend, scanID string) (*models.PolicyResponseModel, error) {
	if b == nil || b.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if scanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "scanID", Reason: "is required"}
	}
	return endpoints.Evaluate(ctx, b.Executor, b.BaseURL, scanID)
}
