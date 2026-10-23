package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
)

// GraphQLRequest is one GraphQL query payload.
//
// The Checkmarx One SCA service uses GraphQL with skip/take pagination for
// its tenant-level analysis surface (CLAUDE.md §10.2). The pagination
// package's GraphQL iterator wraps this type to walk results page by page.
type GraphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// GraphQLResponse is the standard GraphQL response envelope.
//
// Data carries the schema-specific payload as raw bytes so callers can
// decode their own typed shapes without committing this package to a
// particular type. Errors mirrors the spec's per-field error array.
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError is one entry in [GraphQLResponse.Errors] per the GraphQL spec.
type GraphQLError struct {
	Message string         `json:"message"`
	Path    []any          `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// GraphQLClient sends GraphQL queries through the SDK request funnel.
//
// The funnel applies the same authentication, retries, observability, and
// mandatory Checkmarx One headers as for REST calls. GraphQL responses are
// decoded into [GraphQLResponse] and any errors[] entries are surfaced as
// a *cxerrors.ResponseError so callers can use the standard catalog
// discrimination.
//
// Construct one with [NewGraphQLClient]; share it for the lifetime of the
// cxone client. The client is safe for concurrent use.
type GraphQLClient struct {
	executor *Executor
	endpoint string // fully-qualified GraphQL endpoint URL
}

// NewGraphQLClient wraps an [*Executor] with a GraphQL endpoint URL. The
// endpoint is the absolute URL of the GraphQL HTTP handler (e.g.
// "https://ast.checkmarx.net/api/sca/graphql").
func NewGraphQLClient(executor *Executor, endpoint string) *GraphQLClient {
	return &GraphQLClient{executor: executor, endpoint: endpoint}
}

// Execute sends a GraphQL query (and any variables) and decodes a successful
// response's `data` field into out. Returns a *cxerrors.ResponseError when
// the response carries `errors[]` entries (regardless of HTTP status —
// GraphQL servers commonly return 200 with errors in the body).
//
// out should be a pointer to the schema-specific shape that maps the data
// payload; pass nil if you only care about errors. Pass a *json.RawMessage
// if the caller wants to defer schema-aware decoding (the pagination
// package's GraphQL iterator does this).
//
// This method's signature intentionally matches the GraphQLExecutor
// interface in pagination so a *GraphQLClient can be passed to
// pagination.NewGraphQL without an adapter.
func (c *GraphQLClient) Execute(ctx context.Context, query string, variables map[string]any, out any) error {
	if c.endpoint == "" {
		return &cxerrors.ConfigurationError{Field: "GraphQL.Endpoint", Reason: "is required"}
	}
	if query == "" {
		return &cxerrors.ConfigurationError{Field: "GraphQL.Query", Reason: "is required"}
	}
	req := GraphQLRequest{Query: query, Variables: variables}

	var resp GraphQLResponse
	if err := DoJSON(ctx, c.executor, http.MethodPost, c.endpoint, url.Values{}, req,
		[]int{http.StatusOK}, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return &cxerrors.ResponseError{
			Method:     http.MethodPost,
			URL:        c.endpoint,
			StatusCode: http.StatusOK,
			Reason:     fmt.Sprintf("graphql: %s", resp.Errors[0].Message),
		}
	}
	if out == nil || len(resp.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return &cxerrors.ResponseError{
			Method:     http.MethodPost,
			URL:        c.endpoint,
			StatusCode: http.StatusOK,
			Reason:     "decode graphql data: " + err.Error(),
			Cause:      err,
		}
	}
	return nil
}
