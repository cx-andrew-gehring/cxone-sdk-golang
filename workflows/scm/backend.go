// Package scm implements the high-level source-control-management workflow.
//
// The workflow's reason for existing is [Reconnect]: moving an existing
// Checkmarx One project from one code repository to another — possibly on a
// different SCM provider — without recreating the project and losing its scan
// history. That is a two-call sequence (disconnect, then convert) followed by
// polling an asynchronous process, which is exactly the kind of thing
// CLAUDE.md §9 says belongs in the workflow layer rather than in every
// caller.
//
// The polling half follows the state-inspector plus waiter pattern mandated by
// CLAUDE.md §9.4: [ConversionInspector] is built by the async factory
// [FromProcessID] and exposes typed terminal-state predicates;
// [WaitUntilComplete] drives it to a terminal state while honoring
// cancellation.
package scm

import "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"

// Backend bundles the dependencies the SCM workflow needs.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}
