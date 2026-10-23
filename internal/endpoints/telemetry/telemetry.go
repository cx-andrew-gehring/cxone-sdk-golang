// Package telemetry implements the Checkmarx One AI telemetry endpoint.
package telemetry

import (
	"context"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the telemetry endpoint.
const Path = "api/telemetry/log"

// LogAIEvent reports one AI-assisted scan event back to the platform.
// POST /api/telemetry/log → 200.
func LogAIEvent(ctx context.Context, e *transport.Executor, baseURL string, in *models.AITelemetryEvent) error {
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, in, []int{http.StatusOK}, nil)
}
