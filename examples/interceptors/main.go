// Interceptors demonstrates how to register a custom request/response
// interceptor on the SDK client. Interceptors run inside the retry loop on
// every attempt, letting you add headers, log payloads, implement
// circuit-breaker logic, or mock responses in tests.
//
// This example adds a custom X-Request-Source header to every outgoing
// request and logs the response status on the way back.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
)

func main() {
	// InterceptorFunc adapts a pair of plain functions (before / after)
	// to the Interceptor interface.
	headerInjector := cxone.InterceptorFunc{
		BeforeFn: func(ctx context.Context, req *transport.Request, attempt int) error {
			// Add a custom header to every request.
			if req.Header == nil {
				req.Header = make(map[string][]string)
			}
			req.Header.Set("X-Request-Source", "sdk-interceptor-example")
			fmt.Printf("[before] %s %s (attempt %d)\n", req.Method, req.URL, attempt)
			return nil
		},
		AfterFn: func(ctx context.Context, req *transport.Request, resp *transport.Response, err error) error {
			if resp != nil {
				fmt.Printf("[after]  %s %s → %d\n", req.Method, req.URL, resp.StatusCode)
			} else if err != nil {
				fmt.Printf("[after]  %s %s → error: %v\n", req.Method, req.URL, err)
			}
			return err
		},
	}

	client, err := cxone.NewClient().
		Region(cxone.RegionUS).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("interceptor-example").
		APIKey(mustEnv("CX_API_KEY")).
		Interceptors(headerInjector).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	projects, err := client.Advanced().Projects().List(ctx, nil)
	if err != nil {
		log.Fatalf("list projects: %v", err)
	}
	fmt.Printf("\nListed %d projects with interceptor active.\n", projects.TotalCount)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
