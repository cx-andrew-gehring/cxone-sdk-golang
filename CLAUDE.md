# CLAUDE.md — Checkmarx One SDK

> **Audience:** Claude (and human engineers) working in a repo that hosts a Checkmarx One SDK in **any language** — Go, C#, TypeScript, Java, Rust, Python, etc.
>
> **Purpose:** This document is the **architectural contract** every Checkmarx One SDK must satisfy. It is language-neutral and self-contained. Drop it into any disconnected SDK repo and it should be sufficient — together with the public Checkmarx One API documentation — to build a behaviorally consistent SDK.
>
---

## 1. Purpose & Scope

**What you are building:** A first-class, idiomatic SDK that wraps the **Checkmarx One** REST API (and a SCA GraphQL endpoint) for the target language. Checkmarx One is a cloud Application Security platform offering SAST, SCA, IaC, and related scanning. The SDK is intended to be consumed by Professional Services, customers, and integration partners to automate scanning, results retrieval, project/preset/policy management, and reporting.

**This is an SDK, not an API wrapper.** The bar is *not* "one function per endpoint." The bar is:

- Strongly-typed request and response models.
- Fluent builders and domain workflows for the things users actually want to do (scan a repo, scan an upload, fetch results, generate a report, walk presets).
- First-class cancellation and per-call timeouts.
- Pluggable observability (logging, metrics, tracing) and request interceptors.
- Explicit client lifecycle (pooling + dispose/close).
- Idiomatic packaging, semver, and documentation.

The endpoint-per-function layer still exists, but it is **internal / power-user**. The high-level SDK is the front door.

---

## 2. Core Principles (non-negotiable)

1. **Two layers, one front door.**
   - **High-level SDK layer** — the public surface. Domain objects, fluent builders, workflows, polling abstractions, caches. This is what users see in quickstarts and READMEs.
   - **Low-level endpoint layer** — internal/power-user. One function (or static method) per REST endpoint. Reachable for power users, but not promoted in docs.
2. **Native async for the target language.** Use the language's idiomatic async HTTP client and concurrency primitives (`HttpClient`+`Task` in C#, `net/http`+`context.Context` in Go, `fetch`/`undici`+`Promise` in TS, `reqwest`+`tokio` in Rust, `aiohttp`/`httpx` in Python). **Do not** dispatch blocking calls to a worker thread.
3. **Strongly-typed everywhere on the public surface.** Requests, responses, configuration, and results are typed objects. Raw maps/dicts/JSON values are an internal implementation detail at most. Tolerate unknown response fields for forward compatibility — never reject them.
4. **One request funnel.** Every HTTP call in the SDK — high-level or low-level — passes through a single `ExecuteRequest` (or equivalent) method on the client. That funnel applies headers, auth, retry, observability, cancellation, and timeout. Nothing bypasses it.
5. **Cancellation and timeouts on every operation.** Every public async method takes the language's cancellation primitive and supports a per-call timeout.
6. **Observability is mandatory.** Pluggable logger, metrics hooks, tracing spans (OpenTelemetry-compatible), and a request/response interceptor pipeline. Default implementations are no-ops; users wire in real ones.
7. **Explicit lifecycle.** The client implements the language's disposal idiom (`IDisposable`, `io.Closer`, `using`, `close()`). Disposal is idempotent. Pooled connections are released.
8. **No secrets in errors or logs, ever.** Bearer tokens, API keys, refresh tokens, and OAuth secrets must be redacted in every error message, log line, trace attribute, and stringified header dump.
9. **Forward compatibility.** Unknown response fields are tolerated. The SDK never breaks when Checkmarx One adds a property.
10. **Behavioral parity across languages.** The headers, retry semantics, auth flows, region endpoints, and error model defined in this document are identical in every SDK.

---

## 3. Project Layout

Use the language's idiomatic naming for namespaces/packages/modules, but the conceptual layout must be:

```
<sdk-root>/
├── Client/                      # The CxOneClient and its builder/options
├── Auth/                        # Region endpoints, OAuth + API-key flows, token cache
├── Transport/                   # Request funnel, retry, timeout, interceptors
├── Observability/               # Logger, metrics, tracing interfaces + no-op defaults
├── Models/                      # Strongly-typed request and response models
├── Workflows/ (or Operations/)  # High-level SDK: builders, polling, caching, domain objects
│   ├── Projects/
│   ├── Scans/
│   ├── Results/                 # SAST results, SCA results
│   ├── Presets/
│   ├── Reports/
│   ├── Configuration/
│   ├── Policies/
│   ├── AccessManagement/
│   └── Sca/                     # Includes the GraphQL iterator base
├── Pagination/                  # Item-yielding async stream abstractions
├── Internal/Endpoints/          # Low-level layer (internal visibility)
│   ├── Projects/
│   ├── Scans/
│   ├── Uploads/
│   ├── SastResults/
│   ├── SastQueries/
│   ├── Sca/
│   ├── AccessManagement/
│   ├── Policies/
│   ├── Presets/
│   ├── ScanConfiguration/
│   ├── RepoManager/
│   └── Reports/
├── Exceptions/ (or Errors/)     # The error catalog (§7)
├── examples/                    # Runnable example programs (§16)
└── tests/                       # Unit + integration tests (§15)
```

The `Internal/Endpoints` namespace must be marked internal/package-private/non-exported using the language's idiomatic mechanism (e.g. `internal` in C#/Go, no `export` in TS, `pub(crate)` in Rust). Power users can still reach it through an explicit "advanced" entry point if needed, but it is not in the default public API.

---

## 4. The Client

### 4.1 Construction

The client is constructed via a **builder or options object** — never via telescoping constructors.

Required inputs:

- **Tenant name** (string).
- **Region** (typed enum or typed endpoint object — see §13).
- **Credentials** — exactly one of: OAuth client credentials (`clientId` + `clientSecret`) or API key (which is actually a refresh token; see §6.2).
- **Agent name** (string) — appears in the `User-Agent` header and is used by Checkmarx One to identify the integration in scan history and logs.

Optional inputs (with defaults):

| Option | Default | Notes |
|---|---|---|
| `timeoutSeconds` | `60` | Per-request HTTP timeout. |
| `retries` | `3` | Total attempts per request. See §4.4. |
| `retryDelaySeconds` | `15` | Maximum backoff between attempts. |
| `randomizeRetryDelay` | `true` | If true, delay is uniformly random in `[1, retryDelaySeconds]` per attempt. |
| `proxy` | `null` | HTTP/HTTPS proxy. |
| `verifySsl` | `true` | TLS certificate verification. |
| `logger` | no-op | §11. |
| `metrics` | no-op | §11. |
| `tracer` | no-op | §11. |
| `interceptors` | empty | §11. |
| `httpClient` | SDK-managed | Allow users to inject their own pooled HTTP client. |

The builder validates everything up front and throws `ConfigurationException` (or the language's equivalent — see §7) on invalid input.

### 4.2 Required HTTP headers (every request)

These headers must be set on **every** API request, with these exact names and formats:

| Header | Value | Notes |
|---|---|---|
| `Authorization` | `Bearer <access_token>` | From the auth flow (§6). |
| `Accept` | `*/*; version=1.0` | Verbatim. Required by Checkmarx One. |
| `User-Agent` | `<agentName>/(CxOne <LangSDK>/<sdkVersion>)` | Example: `MyIntegration/(CxOne GoSDK/1.4.0)`. |
| `CorrelationId` | A UUID generated **once per client instance** and reused for the lifetime of the client. | Enables Checkmarx-side request tracing. |

Auth flow requests (token endpoint) use a different header set:

| Header | Value |
|---|---|
| `Content-Type` | `application/x-www-form-urlencoded` |
| `Accept` | `application/json` |

### 4.3 The single request funnel

Every HTTP call — internal endpoint functions and high-level workflows alike — must go through one method on the client. Pseudocode:

```
async ExecuteRequest(method, url, body?, queryParams?, headers?, cancellationToken):
    apply timeout (per-call override OR client default)
    open tracing span "cxone.request"
    notify request interceptors
    for attempt in 1..retries:
        ensure auth token (lazy first call, refresh on 401, see §4.5)
        merge headers: standard (§4.2) + per-call
        try:
            response = await httpClient.send(method, url, body, queryParams, headers, cancellationToken)
        catch transient transport error (connection reset, DNS, read/connect timeout, proxy):
            log + metric + maybe retry (see §4.4); rethrow as CommunicationException if exhausted
        if response.status == 401:
            refresh auth token (locked, see §4.5)
            continue            # do NOT count toward retries
        if response.is_success or status not in retryable set:
            notify response interceptors
            close span
            return response
        else:
            log + metric + maybe retry (see §4.4)
    raise CommunicationException(method, url, redacted_args)
```

Nothing in the SDK may bypass this funnel.

### 4.4 Retry policy (verbatim across languages)

**Retry conditions** (any of):

- HTTP status `500`, `502`, `503`, `504`.
- Transient transport exceptions: connection refused/reset, DNS failure, read timeout, connect timeout, proxy error.

**Do not retry on:**

- Any 2xx (return).
- Any 4xx **except** `401` (which triggers re-auth + one retry that does not count toward the retry budget).
- Cancellation — propagate immediately.

**Defaults:** 3 total attempts. Backoff: a uniformly random delay between `1` and `retryDelaySeconds` seconds, applied between attempts. Jitter is required (set `randomizeRetryDelay = true` by default). When `randomizeRetryDelay = false`, the delay is the fixed `retryDelaySeconds` value.

After the final attempt, raise the SDK's `CommunicationException` (§7).

**Optional extension: opt-in 429 retry.** An SDK may offer a builder option (Go: `RetryOnTooManyRequests(maxWait)`) that is **off by default**, so the rules above, and cross-language parity, hold unless a caller turns it on. When on, a `429` is retried within the same attempt budget, waiting the server's `Retry-After` (seconds or HTTP-date) or the normal jittered backoff when it is absent. It is not retried when `Retry-After` exceeds `maxWait` or the call's deadline, or on the last attempt; then the `429` is returned as with the option off. Any method may be retried, because a `429` means the request was not processed.

### 4.5 Auth lifecycle

- **Lazy first auth.** No token is fetched until the first request. The first request triggers a token fetch.
- **401 → refresh → retry.** A `401` response triggers a token refresh and then exactly one transparent retry of the original request (above and beyond the retry budget). If the refresh itself fails, raise `AuthException`.
- **Serialized refresh.** Concurrent requests must serialize on a single token refresh. Use a lock / mutex / `sync.Once` / `SemaphoreSlim` / `asyncio.Lock` so that only one refresh is in flight; other requests wait and reuse the new token.
- **Token cache.** The token is stored on the client instance and reused until a 401 forces refresh. Do not preemptively refresh based on `expires_in` unless you have evidence Checkmarx One requires it.

### 4.6 Cancellation, timeout, and lifecycle

- Every public async method takes the language's cancellation primitive (`CancellationToken`, `context.Context`, `AbortSignal`, etc.) and supports a per-call timeout that overrides the client default for that one call.
- Cancellation must propagate cleanly: no leaked tasks, goroutines, sockets, or file handles.
- The client implements the language's disposal idiom. `Dispose`/`Close` is **idempotent** and releases the underlying HTTP client and any pooled connections. The SDK must not leak resources if disposed mid-flight; in-flight requests should be cancelled.

---

## 5. Models (Strongly Typed — Mandatory)

- Every public request body, response body, query-parameter set, and configuration object is a typed object — `class`/`record`/`struct`/`interface` per the target language. Raw `map`/`dict`/`JSON` types are **not** part of the public API.
- Use the language's idiomatic immutability (records in C#, structs in Go, frozen classes in Python, `readonly` interfaces in TS, `#[derive(Clone)]` in Rust).
- Field naming on the wire follows the Checkmarx One API exactly. Surface field naming follows the language's idiomatic case (PascalCase in C#, camelCase or PascalCase in Go, camelCase in TS). Use the language's serializer attributes/tags to bridge.
- **Tolerate unknown fields** in responses. Configure your serializer to ignore extras. Do not throw on additions.
- A small number of **stable** result objects (e.g. `CxOneVersions`, `PresetDescriptor`, `QueryDescriptor`) may be exposed as immutable value objects with explicit factories.

---

## 6. Authentication

Two flows are supported. Both POST to the OIDC token endpoint:

```
POST {authEndpoint}/protocol/openid-connect/token
Content-Type: application/x-www-form-urlencoded
Accept: application/json
```

The `{authEndpoint}` is the per-region IAM URL (§13), with the realm path appended:

```
https://<region-iam-host>/auth/realms/<tenantName>/protocol/openid-connect/token
```

The successful response is JSON; the `access_token` field is what becomes the `Authorization: Bearer …` header.

### 6.1 OAuth client credentials

Form body:

```
grant_type=client_credentials
client_id=<oauthClientId>
client_secret=<oauthClientSecret>
```

### 6.2 API key (refresh token)

The "API key" presented by Checkmarx One IAM is in fact an OAuth refresh token. The `client_id` is the hardcoded literal `ast-app`.

Form body:

```
grant_type=refresh_token
client_id=ast-app
refresh_token=<apiKey>
```

### 6.3 Auth retry

The auth call itself uses the same retry rules as §4.4 (5xx + transient transport errors, same backoff). On exhaustion, raise `AuthException` with the upstream reason — but **never** include the form body in the error message (it contains secrets).

---

## 7. Error Model

The SDK exposes a fixed catalog of error/exception types. Use the language's idiomatic error mechanism (exceptions in C#/Java/Python/TS, `error` values in Go, `Result` in Rust) but the **names and semantics** below are required:

| Type | Raised when |
|---|---|
| `EndpointException` | Region or endpoint configuration is invalid (bad URL, missing tenant, malformed scheme). |
| `AuthException` | Token acquisition or refresh failed after retries. |
| `CommunicationException` | All retries exhausted on a request, or a non-retryable transport error occurred. Carries the HTTP method, URL, status code (if any), and correlation ID. |
| `ResponseException` | The response was received but could not be parsed, or the status code was not in the allowed set for that operation. |
| `ScanException` | Scan-specific failure surfaced from a high-level workflow (e.g. invalid scan configuration, scan ended in failed state when callers required success). |
| `ConfigurationException` | Runtime validation failure on user-supplied SDK configuration (wrong type, value not in enum, attempted write to read-only setting). |
| `ReportException` (high-level) | Report request rejected, report polling timed out, or report file format unsupported. |

**Mandatory rules:**

- **Bearer token redaction.** Any string of the form `Bearer …` appearing in error messages, args, kwargs, headers, or stringified request representations must be replaced with the literal `REDACTED`. Apply the same to `client_secret`, `refresh_token`, and `api_key` form fields.
- **Cause preservation.** The original transport exception (or wrapped error) must be available as the inner exception / wrapped error / cause.
- **Diagnostic fields.** Every transport-related error must expose: HTTP method, URL (with secrets stripped), status code (if any), attempt number, and the client's correlation ID.

---

## 8. Low-Level Endpoint Layer (Internal)

The low layer mirrors the Checkmarx One REST API surface 1:1.

- **One function (or static method) per endpoint.** Module/file per API area (`Projects`, `Scans`, `Uploads`, `SastResults`, `SastQueries`, `Sca`, `AccessManagement`, `Policies`, `Presets`, `ScanConfiguration`, `RepoManager`, `Reports`, …).
- **Take a typed request, return a typed response.** Raw HTTP response objects must not appear on the public surface — even if power users reach into the internal namespace, they get typed models.
- **Use the request funnel.** Endpoint functions build the URL + body + query string, then call `client.ExecuteRequest(...)`. They never instantiate their own HTTP client.
- **Parameter naming.** Some Checkmarx One query parameters use kebab-case (e.g. `project-ids`, `from-date`, `repo-url`). Surface these as idiomatic identifiers in the SDK and translate at serialization time.
- **Visibility.** Mark this layer internal/package-private. Provide an explicit "advanced" entry point on the client (e.g. `client.Advanced.Endpoints`) if power users need direct access — never the default.

---

## 9. High-Level SDK Layer (the front door)

This is what 95% of users touch. It must offer:

### 9.1 Domain objects with async factories

Constructors cannot perform I/O in any language. For domain objects whose construction requires a server fetch, expose **async factory methods**:

```
projectRepoConfig = await ProjectRepoConfig.FromProjectId(client, projectId, cancellationToken)
```

Not:

```
projectRepoConfig = new ProjectRepoConfig(client, projectId)   // ❌ would have to block or be lazy-broken
```

### 9.2 Fluent builders for complex operations

Operations with many optional inputs (scan invocation, report generation, scan filter configuration, query/preset selection) use a **builder/fluent API** that validates before sending. Example shape (pseudocode):

```
scanId = await client.Scans
    .NewScan(projectRepoConfig)
    .ForBranch("main")
    .WithEngines("sast", "sca")
    .WithTags("ci", "release-candidate")
    .StartAsync(cancellationToken);
```

Builders must validate up front and raise `ConfigurationException` for invalid combinations before any HTTP call.

### 9.3 Lock-protected lazy initialization

Domain objects that cache server data must guarantee a **single fetch** even when multiple callers await concurrently. Pseudocode:

```
async GetData():
    await acquire(this.lock)
    try:
        if not this.fetched:
            this.data    = await fetch(...)
            this.fetched = true
    finally:
        release(this.lock)
    return this.data
```

Use the idiomatic primitive: `Lazy<Task<T>>` / `AsyncLazy` in C#, `sync.Once` in Go, `Promise` memoization in TS, `OnceCell` in Rust, `asyncio.Lock` in Python. Multi-level caches (e.g. presets → query families → queries) use one lock per level.

### 9.4 State inspector + waiter for long-running operations

Server operations that complete asynchronously (scans, reports) must expose:

1. A **state inspector** object created via async factory: `await ScanInspector.FromScanId(client, scanId, ct)`. It exposes typed properties: `Executing`, `Successful`, `Failed`, `StateMessage`, plus the underlying typed scan model.
2. A high-level **waiter convenience**: `await client.Scans.WaitUntilCompleteAsync(scanId, pollIntervalSeconds, cancellationToken)` that polls until the scan reaches a terminal state, surfaces progress events (where the language supports it), and honors cancellation.

**Scan state semantics** (verbatim — these strings come from the Checkmarx One API):

- `Executing` states: `Queued`, `Running`.
- `Failed` states: `Failed`, `Canceled`.
- `Successful` states: `Completed`.
- `Maybe` (partial) state: `Partial` — drill into per-engine `statusDetails` to determine whether the scan is still executing, succeeded, or failed for the engines that were requested.

Inspectors must derive `Executing`/`Successful`/`Failed` correctly even in the `Partial` state by walking per-engine status details and intersecting against the engines that were originally requested.

### 9.5 Multi-level caching (preset reader pattern)

The preset/query catalog is the canonical example of multi-level caching:

- Level 1: list of presets, indexed by name.
- Level 2: query families per engine (standard vs custom).
- Level 3: queries per family, indexed by id.

Each level uses its own lock and lazy-initializes on first access. Parallel fan-out is allowed (and encouraged) to populate independent siblings — use `Task.WhenAll` / `errgroup.Group` / `Promise.all` / `asyncio.gather`.

---

## 10. Pagination

### 10.1 REST pagination

Expose pagination as an **item-yielding async stream**, not a page-at-a-time API:

| Language | Stream type |
|---|---|
| C# | `IAsyncEnumerable<T>` |
| Go | channel + closer, or an iterator type with `Next(ctx) (T, bool, error)` |
| TypeScript | `AsyncIterable<T>` / `AsyncIterableIterator<T>` |
| Python | `async generator` |
| Java | reactive `Flux<T>` or `Flow.Publisher<T>` |
| Rust | `futures::Stream<Item = T>` |

Required parameters/configuration per pagination call:

| Parameter | Meaning |
|---|---|
| `arrayElement` | Key in the JSON response containing the array (or null if the array is the root). |
| `offsetParam` | Name of the offset query parameter (`offset`, `page`, etc.). |
| `offsetInitValue` | Starting offset (usually `0`). |
| `offsetIsByCount` | If `true`, increment offset by the size of the returned page. If `false`, increment by `1` (page-number style). |
| `pageSize` | Page size limit. |
| `pageRetriesMax` | Max retries per page (default `5`). |
| `pageRetryDelaySeconds` | Delay between page retries (default `3`). |
| `keyElementName` | If the response is a dict-of-results (not a list), the key name to inject into each yielded item. |

The stream **yields items, not pages**. It transparently fetches the next page when the in-memory buffer is exhausted, and stops when the API returns an empty page. Per-page retries are independent of the client's main retry budget.

### 10.2 GraphQL pagination (SCA)

The SCA analysis surface uses GraphQL with `skip`/`take` pagination. Expose the same item-yielding async stream contract via a base iterator class. Subclasses configure the query, the variables, and the response root path. Examples to implement: `TenantLicenses`, `TenantPackages`, `TenantRisks`.

---

## 11. Observability & Interceptors (Mandatory)

The SDK must expose pluggable hooks. Default implementations are no-ops; users wire in their own.

### 11.1 Logger interface

Structured log events at minimum:

| Event | Fields |
|---|---|
| `request.start` | method, url (secrets stripped), correlationId, attempt |
| `request.end` | method, url, status, durationMs, correlationId, attempt |
| `request.retry` | method, url, status or exceptionType, nextDelayMs, attempt |
| `auth.refresh` | reason (`initial` or `401`), durationMs |
| `error` | exceptionType, message (sanitized), correlationId |

### 11.2 Metrics hooks

At minimum:

- Request count (counter, dimensioned by method + status).
- Request latency (histogram, dimensioned by method).
- Retry count (counter, dimensioned by reason).
- Auth refresh count (counter).

### 11.3 Tracing

OpenTelemetry-compatible spans around the request funnel and around long-running workflows (scan wait, report wait). Span attributes must include the client's `correlationId`, the HTTP method, the URL host + path (no secrets), the status code, and the attempt number.

### 11.4 Interceptor pipeline

A request/response interceptor pipeline allows users to:

- Add custom headers per request.
- Redact additional fields from logs.
- Mock responses in tests.
- Implement custom retry/circuit-breaker logic that wraps the funnel.

Interceptors run in registration order on the request side and reverse order on the response side.

---

## 12. Concurrency & Cancellation Patterns

- **Shared client state** (token, caches) is protected with the language's idiomatic async-aware mutex/lock.
- **Fan-out** uses the language's idiomatic primitive: `Task.WhenAll`, `errgroup.Group`, `Promise.all`, `asyncio.gather`, `tokio::join!`.
- **Cancellation** propagates through every layer. Cancelling a parent operation cancels its children. No goroutine/task leaks.
- **No global state.** Two `CxOneClient` instances must be fully independent (different tokens, correlation IDs, caches).

---

## 13. Region Endpoint Catalog (verbatim)

Region endpoints must be exposed as **typed objects or a typed enum** — never as free-form strings. The catalog below is canonical and must be inlined in every SDK.

There are nine well-known multi-tenant regions. Each region has two hosts: one for **IAM/Auth** and one for **API**.

| Region key | Auth host (IAM) | API host |
|---|---|---|
| `US`        | `iam.checkmarx.net`        | `ast.checkmarx.net`        |
| `US2`       | `us.iam.checkmarx.net`     | `us.ast.checkmarx.net`     |
| `EU`        | `eu.iam.checkmarx.net`     | `eu.ast.checkmarx.net`     |
| `EU2`       | `eu-2.iam.checkmarx.net`   | `eu-2.ast.checkmarx.net`   |
| `DEU`       | `deu.iam.checkmarx.net`    | `deu.ast.checkmarx.net`    |
| `ANZ`       | `anz.iam.checkmarx.net`    | `anz.ast.checkmarx.net`    |
| `India`     | `ind.iam.checkmarx.net`    | `ind.ast.checkmarx.net`    |
| `Singapore` | `sng.iam.checkmarx.net`    | `sng.ast.checkmarx.net`    |
| `UAE`       | `mea.iam.checkmarx.net`    | `mea.ast.checkmarx.net`    |

URL construction rules:

- **Auth (token) URL:** `https://<authHost>/auth/realms/<tenantName>/protocol/openid-connect/token`
- **Auth admin URL** (used by some IAM operations): `https://<authHost>/auth/admin/realms/<tenantName>/`
- **API base URL:** `https://<apiHost>/api/`
- **Display root URL** (for building UI deep links): `https://<apiHost>/`

The default scheme is `https`. Allow `http` only as an explicit override (e.g. for local Checkmarx test environments). Validate the constructed URL at construction time and raise `EndpointException` on failure.

Custom (non-multi-tenant) deployments are supported by exposing constructors that accept an arbitrary `authHost` + `tenantName` and `apiHost`.

---

## 14. Naming & Style Conventions

Use language-idiomatic casing, but the following **suffix conventions** are part of the cross-language contract because they communicate intent:

| Suffix | Meaning |
|---|---|
| `*Invoker` | Initiates work (e.g. `ScanInvoker` starts a scan and returns a scan id). |
| `*Inspector` | Inspects state of an existing entity (e.g. `ScanInspector` exposes scan status). |
| `*Reader` | Cached lookup over a remote catalog (e.g. `PresetReader`). |
| `*Config` | Typed configuration view, often lazily loaded (e.g. `ProjectRepoConfig`, `ScanFilterConfig`). |
| `*Builder` | Fluent construction of a request or workflow input. |

Endpoint function names in the internal layer mirror the Checkmarx One API operation phrasing: `CreateAProject`, `RetrieveListOfProjects`, `RetrieveProjectInfo`, `UpdateAProject`, `DeleteAProject`, `RetrieveLastScan`, `RetrieveListOfBranches`, etc. — adapted to the language's casing.

---

## 15. Testing Requirements

- **Dual-auth test base.** Provide a base test class/fixture that constructs **two clients** — one with OAuth credentials, one with an API key — and runs every integration test against **both** in parallel via the language's fan-out primitive (`Task.WhenAll`, `errgroup`, etc.). This catches auth-flow regressions early.
- **Credential injection via environment variables.** Tests load `TEST_OAUTH_CLIENT_ID`, `TEST_OAUTH_CLIENT_SECRET`, `TEST_API_KEY`, `TEST_TENANT_ID`, `TEST_REGION` (and equivalents) from the environment or a `.env` file. Tests are skipped when credentials are absent — never hard-fail in CI without them.
- **Cleanup hooks.** Use the language's async setup/teardown to delete projects, presets, and scans created during a test, even on failure.
- **Unit tests for the funnel.** Cover: 401 → refresh → retry, 5xx → retry → success, 5xx → retry → exhaustion → `CommunicationException`, transient transport error → retry, cancellation propagation, secret redaction in errors. Use an in-process HTTP mock (WireMock, MSW, httptest, responses, etc.).
- **Pagination tests.** Cover both offset-by-count and offset-by-page-number, empty pages, mid-stream errors with retry, and cancellation mid-stream.
- **Polling tests.** Cover the `Partial` scan state transitions for at least: all engines complete → success; one engine failed → failure; one engine still running → executing.

---

## 16. Packaging, Versioning, Documentation

### 16.1 Semver and stability

- Follow Semantic Versioning. Breaking changes are major-version bumps only.
- Define the public API surface explicitly using the language's mechanism: `internal` keyword (C#), unexported identifiers (Go), absence of `export` (TS), `pub(crate)` (Rust), `__all__` and underscore prefix (Python). Internal APIs may change at any time without a major bump.
- Deprecation policy: mark with the language's deprecation attribute (`[Obsolete]`, `// Deprecated:`, `@deprecated`, `#[deprecated]`, `@deprecation.deprecated`), keep working for one full major version, then remove.

### 16.2 Package naming

| Language | Package name |
|---|---|
| C# / .NET | `Checkmarx.CxOne.Sdk` |
| Go | `github.com/checkmarx-ts/cxone-go-sdk` (or org-appropriate) |
| TypeScript / Node | `@checkmarx/cxone-ts-sdk` |
| Python | `cxone_sdk` (or `cxone_python_sdk` for the reference) |
| Java | `com.checkmarx.cxone.sdk` |
| Rust | `cxone-sdk` |

### 16.3 README structure (required sections, in order)

1. Badges (build, package version, license).
2. One-paragraph description ("Async SDK for Checkmarx One in <language>").
3. Installation.
4. 60-second quickstart: construct client → start a scan → wait → fetch results.
5. Authentication: both OAuth and API-key examples, with environment-variable patterns.
6. Region selection (link to §13).
7. Examples directory pointer.
8. Observability: how to wire a logger / metrics / tracer.
9. Cancellation and timeout patterns.
10. Error handling: the catalog from §7.
11. Versioning and stability policy.
12. Link to Checkmarx One public API documentation.

### 16.4 Required runnable examples

Each SDK ships an `examples/` directory with at least:

| Example | What it shows |
|---|---|
| `quickstart` | Construct client, list projects. |
| `scan-repo` | Create project, scan a Git repo, wait for completion, print state. Uses the high-level workflow. |
| `scan-upload` | Create project, upload a zip, scan the upload, wait for completion. |
| `paginated-list` | Iterate paginated results with cancellation support. |
| `report-generation` | Request a report, poll until ready, download. |
| `observability` | Wire a logger, OpenTelemetry tracer, and metrics provider into the client. |
| `interceptors` | Custom interceptor for adding a header or redacting a field. |

---

## 17. Porting Checklist

A new-language SDK is "done" when **every** box is ticked:

**Client & transport**
- [ ] Builder/options pattern for client construction with all options from §4.1.
- [ ] Region enum/typed object with all 9 entries from §13, validated up front.
- [ ] Single `ExecuteRequest` funnel that applies headers, auth, retry, observability, cancellation, and timeout.
- [ ] Required headers from §4.2 set on every request, with the exact `Accept: */*; version=1.0` value.
- [ ] Per-instance `CorrelationId` UUID, generated once at construction, sent on every request.
- [ ] Retry policy from §4.4 (status codes + transient exceptions + 3 attempts + randomized backoff).
- [ ] Lazy auth, 401 → refresh → retry, refresh serialized via lock.
- [ ] Cancellation token / context on every public async method.
- [ ] Per-call timeout overrides client default.
- [ ] Idempotent client disposal that releases the HTTP client and pooled connections.

**Auth**
- [ ] OAuth client_credentials flow per §6.1.
- [ ] API-key (refresh_token grant, hardcoded `client_id=ast-app`) flow per §6.2.
- [ ] Auth retry uses §4.4 rules.
- [ ] Form bodies never appear in error messages or logs.

**Models & errors**
- [ ] Strongly-typed models for every public request and response.
- [ ] Unknown response fields tolerated (forward compatible).
- [ ] Error catalog from §7 with bearer/secret redaction and cause preservation.

**Layers**
- [ ] Internal endpoint layer with one function per Checkmarx One endpoint, organized by API area.
- [ ] Internal endpoint layer is non-public by default; reachable only via an explicit advanced entry point.
- [ ] High-level workflow layer is the front door.
- [ ] Async factory methods on domain objects that need remote data (no I/O in constructors).
- [ ] Fluent builder for scan invocation, with up-front validation.
- [ ] `ScanInspector` with `Executing`/`Successful`/`Failed`/`StateMessage` honoring all states from §9.4 including `Partial`.
- [ ] `WaitUntilComplete` waiter convenience that polls and honors cancellation.
- [ ] `PresetReader` (or equivalent) implementing the multi-level caching pattern from §9.5.
- [ ] Lock-protected lazy init pattern from §9.3 used wherever a domain object caches server data.

**Pagination**
- [ ] REST pagination as an item-yielding async stream supporting both offset styles.
- [ ] Per-page retry independent of the request funnel's retry budget.
- [ ] GraphQL (SCA) iterator base supporting `skip`/`take`.

**Observability**
- [ ] Pluggable logger with the structured events from §11.1 (defaults to no-op).
- [ ] Metrics hooks from §11.2 (defaults to no-op).
- [ ] OpenTelemetry-compatible tracing spans (defaults to no-op).
- [ ] Request/response interceptor pipeline.

**Tests**
- [ ] Dual-auth integration test base (every integration test runs against both OAuth and API key in parallel).
- [ ] Unit tests for: 401 refresh, 5xx retry, transport-error retry, exhaustion, cancellation, redaction.
- [ ] Pagination unit tests for both offset styles + cancellation.
- [ ] Polling tests for `Partial` state transitions.
- [ ] Cleanup hooks remove test artifacts even on failure.

**Packaging & docs**
- [ ] Semver + deprecation policy documented.
- [ ] Public API surface explicitly delineated using the language's visibility primitives.
- [ ] README contains all sections from §16.3.
- [ ] `examples/` contains all entries from §16.4.
- [ ] Package name follows §16.2.
