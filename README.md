# cxone-sdk-golang

[![Go Reference](https://pkg.go.dev/badge/github.com/checkmarx-open-labs/cxone-sdk-golang.svg)](https://pkg.go.dev/github.com/checkmarx-open-labs/cxone-sdk-golang)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)
![Version](https://img.shields.io/badge/version-0.9.0--rc.1-orange)

Idiomatic Go SDK for the [Checkmarx One](https://checkmarx.com/product/application-security-platform/) application-security platform. Automate scanning, results retrieval, project/preset/policy management, user provisioning, and reporting from any Go application.

---

## Table of Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Quickstart](#quickstart)
- [Authentication](#authentication)
- [Region Selection](#region-selection)
- [Examples](#examples)
- [Observability](#observability)
- [Cancellation & Timeouts](#cancellation--timeouts)
- [High-Level Workflows](#high-level-workflows)
- [Error Handling](#error-handling)
- [Project Structure](#project-structure)
- [Testing](#testing)
- [Versioning & Stability](#versioning--stability)
- [Contributing](#contributing)
- [License](#license)
- [Reference](#reference)

---

## Requirements

- **Go 1.24** or later
- A [Checkmarx One](https://checkmarx.com/product/application-security-platform/) tenant with either OAuth client credentials or an API key

## Installation

```sh
go get github.com/checkmarx-open-labs/cxone-sdk-golang
```

## Quickstart

Construct a client, list projects, start a scan, and wait for results in under 60 seconds:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    cxone "github.com/checkmarx-open-labs/cxone-sdk-golang"
    "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scans"
)

func main() {
    client, err := cxone.NewClient().
        Region(cxone.RegionUS).
        Tenant("acme").
        AgentName("MyApp").
        APIKey(os.Getenv("CX_API_KEY")).
        Build()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    // Load a project and start a SAST scan.
    repo, _ := client.Projects().Get(ctx, "project-uuid")
    insp, _ := client.Scans().NewScan(repo).
        ForBranch("main").
        WithEngines("sast").
        Start(ctx)

    // Wait until the scan completes.
    final, _ := scans.WaitUntilComplete(ctx, insp, scans.WaitOptions{
        PollInterval: 10 * time.Second,
    })
    fmt.Println("Scan", final.ID(), "=>", final.StateMessage())
}
```

## Authentication

The SDK supports both authentication flows offered by Checkmarx One IAM.

### OAuth client credentials

```go
client, err := cxone.NewClient().
    Region(cxone.RegionUS).
    Tenant("acme").
    AgentName("MyIntegration").
    OAuth(os.Getenv("CX_CLIENT_ID"), os.Getenv("CX_CLIENT_SECRET")).
    Build()
```

### API key (refresh token)

```go
client, err := cxone.NewClient().
    Region(cxone.RegionUS).
    Tenant("acme").
    AgentName("MyIntegration").
    APIKey(os.Getenv("CX_API_KEY")).
    Build()
```

Token acquisition is lazy -- no network call happens until the first API request. On a `401` response the SDK refreshes the token and retries once (off the retry budget). Concurrent requests serialize on a single refresh so the IAM endpoint is never hammered.

### Environment variables

| Variable | Used by |
|---|---|
| `CX_TENANT` | Both flows |
| `CX_API_KEY` | API key flow |
| `CX_CLIENT_ID` + `CX_CLIENT_SECRET` | OAuth flow |
| `CX_REGION` | Region selection (US, EU, EU2, ...) |

## Region Selection

Nine multi-tenant regions are exposed as typed constants:

| Constant | Auth Host | API Host |
|---|---|---|
| `RegionUS` | `iam.checkmarx.net` | `ast.checkmarx.net` |
| `RegionUS2` | `us.iam.checkmarx.net` | `us.ast.checkmarx.net` |
| `RegionEU` | `eu.iam.checkmarx.net` | `eu.ast.checkmarx.net` |
| `RegionEU2` | `eu-2.iam.checkmarx.net` | `eu-2.ast.checkmarx.net` |
| `RegionDEU` | `deu.iam.checkmarx.net` | `deu.ast.checkmarx.net` |
| `RegionANZ` | `anz.iam.checkmarx.net` | `anz.ast.checkmarx.net` |
| `RegionIndia` | `ind.iam.checkmarx.net` | `ind.ast.checkmarx.net` |
| `RegionSingapore` | `sng.iam.checkmarx.net` | `sng.ast.checkmarx.net` |
| `RegionUAE` | `mea.iam.checkmarx.net` | `mea.ast.checkmarx.net` |

Custom (single-tenant) deployments:

```go
region, err := cxone.NewCustomRegion("iam.example.com", "ast.example.com")
```

See [`region.go`](./region.go) for URL-construction helpers and validation.

## Examples

The [`examples/`](./examples/) directory ships runnable programs that demonstrate the SDK end-to-end:

| Example | What it shows |
|---|---|
| [`quickstart`](./examples/quickstart/) | Construct client, list projects |
| [`scan-repo`](./examples/scan-repo/) | Create project, scan a Git repo, wait for completion, print state |
| [`scan-upload`](./examples/scan-upload/) | Upload a zip, scan the upload, wait for completion |
| [`paginated-list`](./examples/paginated-list/) | Iterate paginated results with `Iterator[T]`, range-over-func, and `Collect` |
| [`report-generation`](./examples/report-generation/) | Request a report, poll until ready, download |
| [`observability`](./examples/observability/) | Wire a `slog` logger into the client |
| [`interceptors`](./examples/interceptors/) | Custom interceptor for adding a header and logging responses |
| [`users-provisioning`](./examples/users-provisioning/) | UserReader cached lookups, GetOrCreateByEmail |
| [`role-assignment`](./examples/role-assignment/) | RoleReader, composite resolution, ast-app client roles |
| [`analytics-dashboard`](./examples/analytics-dashboard/) | Severity KPIs, MTTR, most-common vulnerabilities |
| [`import-migration`](./examples/import-migration/) | List imports, start import, poll with ImportInspector |
| [`scan-comparison`](./examples/scan-comparison/) | Diff two scans into new/resolved/recurrent counts by severity, plus not-exploitable summary |
| [`scm-disconnect`](./examples/scm-disconnect/) | Disconnect a project from its SCM repository|

Run any example:

```sh
export CX_TENANT=acme CX_API_KEY=... CX_REGION=US
go run ./examples/quickstart
```

## Observability

The SDK ships pluggable `Logger`, `Metrics`, and `Tracer` interfaces under [`observability/`](./observability/). Defaults are no-ops; wire your own via the builder:

```go
client, _ := cxone.NewClient().
    Logger(observability.NewSlogLogger(slog.Default())).
    Metrics(myPrometheusAdapter).
    Tracer(myOtelAdapter).
    // ...
    Build()
```

Structured events emitted on every request:

| Event | Fields |
|---|---|
| `cxone.request.start` | method, url (sanitized), correlationId, attempt |
| `cxone.request.end` | method, url, status, durationMs, correlationId, attempt |
| `cxone.request.retry` | method, url, status or error, attempt |
| `cxone.auth.refresh` | reason (`initial` or `401`), durationMs |
| `cxone.scan.poll` | scanId, status, stateMessage |
| `cxone.report.poll` | reportId, status |
| `cxone.error` | exceptionType, message (sanitized), correlationId |

## Cancellation & Timeouts

Every public method takes `context.Context` as its first argument. Per-call timeouts override the client default:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
project, err := client.Advanced().Projects().Get(ctx, "id")
```

The client-level default timeout is 60 seconds (configurable via `NewClient().Timeout(...)`).

## High-Level Workflows

### Users

```go
// Cached reader -- fetches once, lookups are free.
reader := client.Users().NewReader()
all, _ := reader.List(ctx)
user, _ := reader.ByEmail(ctx, "alice@example.com")

// Idempotent provisioning.
user, _ = client.Users().GetOrCreateByEmail(ctx, &models.User{
    Email: "alice@example.com", Username: "alice", Enabled: true,
})
```

### Roles

```go
reader := client.Roles().NewReader()
all, _ := reader.List(ctx)             // realm roles, cached
subs, _ := reader.Composites(ctx, id)  // lazy per-role L2 cache

// Client-scoped (application) roles.
appRoles, _ := reader.ListClientRoles(ctx, astAppID)
```

### OIDC Clients

```go
oidc, _ := client.Clients().GetOrCreateByName(ctx, "my-automation", nil)
astAppID, _ := client.Clients().ASTAppID(ctx) // cached after first call
```

### Groups

```go
group, _ := client.Groups().GetOrCreateByName(ctx, "my-team")
```

### Projects

```go
repo, _ := client.Projects().GetOrCreateByName(ctx, &models.Project{Name: "my-project"})
repo, app, _ := client.Projects().GetOrCreateInApplicationByName(ctx, "my-project", "my-app")
```

### Scan Comparison

Diff two scans into typed new/resolved/recurrent buckets by severity. The
not-exploitable list is extracted from the new scan, grouped by query name.

```go
cmp, err := client.Scans().Compare(ctx, oldScanID, newScanID)
if err != nil {
    return err
}
fmt.Printf("HIGH: new=%d resolved=%d recurrent=%d\n",
    cmp.BySeverity["HIGH"].New,
    cmp.BySeverity["HIGH"].Resolved,
    cmp.BySeverity["HIGH"].Recurrent)
```

Options: `WithIdentityKey(models.IdentityKeySimilarityID|IdentityKeyResultHash)`,
`WithSeverities(...)`, `WithEngines(...)`, `WithProgress(cb)`. Default identity
is `similarityId`, which matches the legacy CxSAST SOAP `GetScanCompareSummary`
semantics.

### Results

```go
all, _ := client.Results().ListAll(ctx, scanID, nil)
notExp, _ := client.Results().ListByState(ctx, scanID, "NOT_EXPLOITABLE")

for r, err := range client.Results().Iter(scanID, nil).All(ctx) {
    if err != nil { return err }
    // process r
}
```

### Analytics

```go
analytics := client.Advanced().Analytics()
severity, _ := analytics.VulnerabilitiesBySeverityTotal(ctx, models.AnalyticsFilter{})
mttr, _ := analytics.MeanTimeToResolution(ctx, models.AnalyticsFilter{})
common, _ := analytics.MostCommonVulnerabilities(ctx, 10, models.AnalyticsFilter{})
```

### Audit Sessions

```go
sess, _ := client.Advanced().Audit().CreateSession(ctx, &models.AuditCreateRequest{
    Scanner: "sast", Filter: "go",
})
insp, _ := client.Audit().WrapSession(sess)
defer insp.Delete(ctx)

val, _ := audit.WaitForRequest(ctx, insp, sess.Data.RequestID, audit.WaitOptions{
    PollInterval: 5 * time.Second,
})
```

### Data Import / Migration

```go
migrationID, _ := client.Advanced().Migration().StartImport(ctx, "archive.zip", "", "encryption-key")
insp, _ := client.Migration().GetImport(ctx, migrationID)
final, _ := migration.WaitUntilComplete(ctx, insp, migration.WaitOptions{
    PollInterval: 15 * time.Second,
})
fmt.Println(final.Status()) // "completed" or "partial"
```

## Error Handling

The SDK exposes a fixed catalog of typed errors -- use `errors.As` to discriminate:

```go
var commErr *cxone.CommunicationError
if errors.As(err, &commErr) {
    log.Printf("status=%d attempt=%d correlationId=%s",
        commErr.StatusCode, commErr.Attempt, commErr.CorrelationID)
}
```

| Type | Raised when |
|---|---|
| `EndpointError` | Region/endpoint configuration is invalid |
| `AuthError` | Token acquisition or refresh failed after retries |
| `CommunicationError` | All retries exhausted on a request, or non-retryable transport error |
| `ResponseError` | Response could not be parsed or unexpected status code |
| `ScanError` | Scan-specific workflow failure (invalid config, terminal failure) |
| `ConfigurationError` | Invalid SDK configuration (missing field, bad enum) |
| `ReportError` | Report rejected, polling timed out, or unsupported format |

Bearer tokens and form-body secrets are **redacted** from every error message, log line, and trace attribute.

## Project Structure

```
cxone-sdk-golang/
├── cxone.go              # Package entry, version constant
├── client.go             # Client + ClientBuilder (public entry point)
├── region.go             # Typed region constants and URL construction
├── errors.go             # Public error type aliases
├── auth/                 # OAuth and API-key authentication
├── models/               # Strongly-typed request/response models
├── workflows/            # High-level SDK layer
│   ├── scans/            #   Scan invoker, inspector, waiter
│   ├── projects/         #   Project CRUD, GetOrCreate
│   ├── presets/          #   Multi-level cached preset reader
│   ├── reports/          #   Report request, poll, download
│   ├── users/            #   User provisioning and cached reader
│   ├── roles/            #   Role reader with composite caching
│   ├── clients/          #   OIDC client management
│   ├── groups/           #   Group management
│   ├── audit/            #   Query-editor audit sessions
│   └── migration/        #   Data import/migration
├── pagination/           # Generic Iterator[T] and GraphQL iterator
├── observability/        # Logger, Metrics, Tracer interfaces + no-ops
├── advanced/             # Low-level endpoint access handle
├── internal/             # Non-public implementation
│   ├── endpoints/        #   One package per API area (30 packages)
│   ├── transport/        #   Request executor, auth flow, retry
│   ├── retry/            #   Retry policy and backoff
│   └── redact/           #   Secret redaction
├── cxerrors/             # Typed error catalog
├── examples/             # 11 runnable example programs
└── tests/integration/    # Integration test suite
```

## Testing

### Running unit tests

```sh
go test ./...
```

Unit tests cover the request funnel (401 refresh, 5xx retry, transport-error retry, exhaustion, cancellation, secret redaction), pagination (both offset styles, empty pages, mid-stream errors), and polling state transitions.

### Running integration tests

Integration tests require a live Checkmarx One tenant. Set the following environment variables:

| Variable | Required | Description |
|---|---|---|
| `TEST_OAUTH_CLIENT_ID` | For OAuth tests | OAuth client ID |
| `TEST_OAUTH_CLIENT_SECRET` | For OAuth tests | OAuth client secret |
| `TEST_API_KEY` | For API-key tests | Checkmarx One API key |
| `TEST_TENANT_ID` | Yes | Tenant name |
| `TEST_REGION` | Yes | Region key (US, EU, etc.) |

```sh
export TEST_TENANT_ID=acme TEST_API_KEY=... TEST_REGION=US
go test ./tests/integration/... -v
```

Integration tests run against **both** OAuth and API-key clients in parallel (dual-auth test base). Tests are automatically skipped when the required credentials are absent. Cleanup hooks ensure test-created projects, presets, and scans are removed even on failure.

### Test tags

Integration tests use build tags to prevent accidental execution in CI without credentials:

```sh
go test -tags=integration ./tests/integration/...
```

## Versioning & Stability

This project follows [Semantic Versioning](https://semver.org/). Breaking changes are major-version bumps only.

**Current version:** `0.9.0`

**Public API surface:** Everything reachable **without** importing a package whose path contains `/internal/`. Internal packages may change at any time without a major bump.

**Deprecation policy:** Deprecated APIs are marked with Go's `// Deprecated:` comment convention, kept working for one full major version, then removed.

## Contributing

Contributions are welcome. To get started:

1. Fork the repository.
2. Create a feature branch (`git checkout -b feature/my-change`).
3. Make your changes and ensure tests pass (`go test ./...`).
4. Run `go vet ./...` and ensure no lint issues.
5. Commit with a clear message describing the change.
6. Open a pull request against `main`.

Please keep changes focused -- one logical change per PR. If you're planning a large change, open an issue first to discuss the approach.

## License

This project is licensed under the [Apache License 2.0](./LICENSE).

## Reference

- [Checkmarx One API documentation](https://ast.checkmarx.net/spec/v1)
- [Go package documentation](https://pkg.go.dev/github.com/checkmarx-open-labs/cxone-sdk-golang)
- [`CLAUDE.md`](./CLAUDE.md) -- cross-language SDK architectural contract
