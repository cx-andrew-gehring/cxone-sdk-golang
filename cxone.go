// Package cxone is the official Go SDK for the Checkmarx One application
// security platform.
//
// The SDK exposes two layers:
//
//   - A high-level "front door" of fluent builders, domain inspectors, waiters,
//     and cached readers under the workflows/ subpackages. This is what
//     applications should use 95% of the time.
//
//   - A low-level endpoint layer that mirrors the Checkmarx One REST API 1:1.
//     It lives under internal/endpoints/... and is reachable only through the
//     opt-in Advanced handle on the client.
//
// Construction is done via [ClientBuilder]. Every public method takes a
// [context.Context] as its first argument and supports per-call timeouts.
// All requests pass through a single funnel that applies authentication,
// retries, observability, and the mandatory Checkmarx One headers.
//
// See the README and the examples/ directory for end-to-end usage.
package cxone

// Version is the SDK release version. It is reported in the User-Agent header
// on every outgoing request and in tracing/span attributes.
const Version = "0.12.1"

// productName is the SDK identifier embedded in the User-Agent header per
// CLAUDE.md §4.2: "<agentName>/(CxOne <LangSDK>/<sdkVersion>)".
const productName = "GoSDK"
