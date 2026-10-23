// Observability demonstrates how to wire a structured logger, an
// OpenTelemetry-compatible tracer, and a metrics sink into the SDK client.
// The SDK emits events at each step of the request lifecycle; this example
// routes them through Go's slog package so every HTTP call is visible in
// the application's log stream.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/observability"
)

func main() {
	// Wire Go's slog as the SDK's structured logger. Every request
	// start/end, retry, auth refresh, and error event appears as a
	// structured slog record:
	//
	//   INFO cxone.request.start method=GET url=https://ast.checkmarx.net/api/projects ...
	//   INFO cxone.request.end   method=GET url=... status=200 durationMs=142 ...
	//
	slogLogger := observability.NewSlogLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	// For metrics and tracing the SDK ships no-op defaults. To wire real
	// providers, implement observability.Metrics and observability.Tracer
	// and pass them to the builder:
	//
	//   .Metrics(myPrometheusAdapter)
	//   .Tracer(myOtelAdapter)
	//
	// The interfaces are deliberately minimal (one-method-per-concern) so
	// adapters are trivial to write. See observability/metrics.go and
	// observability/tracing.go for the contracts.

	client, err := cxone.NewClient().
		Region(cxone.RegionUS).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("observability-example").
		APIKey(mustEnv("CX_API_KEY")).
		Logger(slogLogger).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Make a simple API call — the slog output on stderr will show the
	// full request lifecycle.
	projects, err := client.Advanced().Projects().List(ctx, nil)
	if err != nil {
		log.Fatalf("list projects: %v", err)
	}
	fmt.Printf("Listed %d projects (check stderr for structured log events)\n", projects.TotalCount)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
