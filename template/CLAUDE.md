# CLAUDE.md — Building with the Checkmarx One Go SDK

> **Audience:** AI assistants (and human engineers) working in a project that consumes the `cxone-sdk-golang` SDK.
>
> **Purpose:** Ensure every code change uses the SDK idiomatically — correct client lifecycle, error handling, concurrency, and layer selection.

---

## SDK Overview

This project uses [`github.com/cx-andrew-gehring/cxone-sdk-golang`](https://github.com/cx-andrew-gehring/cxone-sdk-golang), the idiomatic Go SDK for the Checkmarx One application-security platform. The SDK has **two layers**:

- **High-level workflow layer** (`client.Projects()`, `client.Scans()`, `client.Presets()`, `client.Users()`, etc.) — fluent builders, inspectors, waiters, cached readers. This is the front door for 95% of use cases.
- **Low-level Advanced layer** (`client.Advanced().Projects()`, `client.Advanced().Scans()`, etc.) — 1:1 endpoint wrappers. Use only when the workflow layer doesn't cover your use case.

Always prefer the workflow layer. Fall back to Advanced only when necessary.

---

## Rules

### Client Construction

1. **Always use the builder pattern.** Never construct the client with struct literals.
   ```go
   client, err := cxone.NewClient().
       Region(cxone.RegionUS).
       Tenant("my-tenant").
       AgentName("my-tool").
       APIKey(os.Getenv("CX_API_KEY")).
       Build()
   ```

2. **All four fields are required:** Region (typed constant), Tenant, AgentName, and exactly one of APIKey or OAuth. Missing any causes `Build()` to return a `ConfigurationError`.

3. **Always `defer client.Close()`.** The client owns a pooled HTTP client and a token cache. Failing to close leaks connections.

4. **The client is safe to share across goroutines.** Create one, share it, close it at shutdown. Never use a client after `Close()`.

5. **Do not create multiple clients** unless you genuinely need independent auth tokens or correlation IDs.

### Authentication

6. **Auth is lazy.** No token is fetched until the first API call. A 401 triggers an automatic token refresh + one transparent retry (off the retry budget). You do not need to handle token lifecycle.

7. **Two auth flows — pick one:**
   - `APIKey(key)` — the "API key" is actually an OAuth refresh token. Client ID is hardcoded to `ast-app`.
   - `OAuth(clientID, clientSecret)` — standard client_credentials grant.

8. **Never log or expose credentials.** The SDK redacts bearer tokens and secrets from all errors, logs, and traces automatically. Do not circumvent this.

### Context & Cancellation

9. **Every SDK call takes `context.Context` as its first argument.** Always pass a context — never `context.TODO()` in production code.

10. **Use `context.WithTimeout` for per-call timeouts.** The client default is 60 seconds. Override per-call when appropriate:
    ```go
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    ```

11. **Cancellation propagates cleanly.** Cancelling a context cancels in-flight HTTP requests with no leaked goroutines.

### Error Handling

12. **Use `errors.As` to discriminate error types.** The SDK exposes a fixed catalog:
    | Type | When |
    |---|---|
    | `*cxerrors.ConfigurationError` | Invalid SDK config (missing field, bad enum) |
    | `*cxerrors.EndpointError` | Bad region/URL configuration |
    | `*cxerrors.AuthError` | Token acquisition or refresh failed |
    | `*cxerrors.CommunicationError` | Retries exhausted or transport error |
    | `*cxerrors.ResponseError` | Unexpected status code or unparseable body |
    | `*cxerrors.ScanError` | Scan-specific workflow failure |
    | `*cxerrors.ReportError` | Report-specific failure |

    ```go
    var commErr *cxerrors.CommunicationError
    if errors.As(err, &commErr) {
        log.Printf("status=%d attempt=%d", commErr.StatusCode, commErr.Attempt)
    }
    ```

13. **Never swallow errors silently.** If an SDK call returns an error, either handle it or propagate it. The error always carries a correlation ID for server-side tracing.

### Retry & Resilience

14. **The SDK retries automatically.** 3 attempts with randomized backoff on 5xx and transient transport errors. 4xx (except 401) fail immediately. Do not add your own retry wrapper around SDK calls.

15. **401 is handled transparently.** The SDK refreshes the token and retries once, outside the retry budget. You do not need to catch 401s.

### Using the Workflow Layer

16. **Fluent builders for complex operations.** Scan invocation, report generation, and similar multi-option operations use builder chains that validate before sending:
    ```go
    insp, err := client.Scans().NewScan(repo).
        ForBranch("main").
        WithEngines("sast", "sca").
        Start(ctx)
    ```

17. **Inspectors for state queries.** Long-running operations (scans, reports, imports) return inspectors with typed properties: `Executing()`, `Successful()`, `Failed()`, `StateMessage()`.

18. **Waiters for polling.** Use `scans.WaitUntilComplete(ctx, insp, opts)` and `migration.WaitUntilComplete(ctx, insp, opts)` instead of hand-rolling poll loops. They handle cancellation, keep-alive, and terminal-state detection.

19. **Readers for cached catalogs.** `client.Presets()`, `client.Users().NewReader()`, `client.Roles().NewReader()` lazy-load and cache data. Reuse one reader instance — don't construct a new one per call.

20. **Async factories, not I/O in constructors.** Domain objects that need remote data use factory functions: `wfprojects.FromProjectID(ctx, backend, id)`. Never call a constructor that would need to block.

### Using the Advanced Layer

21. **Advanced is the escape hatch, not the default.** Use `client.Advanced()` only when the workflow layer doesn't cover your endpoint.

22. **Advanced returns typed models.** Even though it's "low-level", every call returns strongly-typed Go structs — never raw JSON or `map[string]any`.

23. **Query parameters use `url.Values`.** List and filter endpoints accept `url.Values` for query strings. The SDK handles URL encoding.

### Observability

24. **Wire observability via the builder.** The SDK ships no-op defaults. Pass your logger/metrics/tracer at construction:
    ```go
    cxone.NewClient().
        Logger(myLogger).
        Metrics(myMetrics).
        Tracer(myTracer).
        // ...
        Build()
    ```

25. **Structured log events are emitted automatically.** `request.start`, `request.end`, `request.retry`, `auth.refresh`, `scan.poll`, `report.poll`, `error` — all with correlation IDs.

### Models & Types

26. **All public request/response types live in `models/`.** Import `github.com/cx-andrew-gehring/cxone-sdk-golang/models` for typed payloads.

27. **Unknown response fields are tolerated.** The SDK's JSON decoder ignores extras for forward compatibility. Do not validate response structs against a fixed schema.

28. **Use the SDK's typed Region constants.** `cxone.RegionUS`, `cxone.RegionEU`, etc. Never hardcode host URLs.

### Concurrency

29. **Shared client state (token, caches) is internally synchronized.** You do not need external locks around SDK calls.

30. **Fan-out uses `errgroup.Group` or `sync.WaitGroup`.** When making concurrent SDK calls, use standard Go concurrency primitives. The client is safe for concurrent use.

---

## Anti-Patterns

- **Do not** create a new client per request — reuse one for the application lifetime.
- **Do not** add retry/backoff wrappers — the SDK handles this.
- **Do not** parse raw HTTP responses — use the typed models.
- **Do not** hardcode Checkmarx One hostnames — use `Region` constants.
- **Do not** log the client's bearer token or any credential field.
- **Do not** call `context.TODO()` in production paths — always derive from a parent context.
- **Do not** ignore errors from `client.Close()`.
