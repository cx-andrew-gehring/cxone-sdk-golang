# GEMINI.md — Gemini Instructions for Checkmarx One Go SDK Projects

This project uses the `cxone-sdk-golang` Go SDK to interact with the Checkmarx One application-security platform.

## SDK Architecture

The SDK has two layers:
1. **Workflow layer** (preferred) — `client.Scans()`, `client.Projects()`, `client.Presets()`, `client.Users()`, `client.Roles()`, `client.Clients()`, `client.Reports()`, `client.Audit()`, `client.Migration()`, `client.Groups()`
2. **Advanced layer** (escape hatch) — `client.Advanced().Projects()`, etc. Use only when workflows don't cover your endpoint.

## Mandatory Patterns

### Client Lifecycle
```go
client, err := cxone.NewClient().
    Region(cxone.RegionUS).      // typed constant, never hardcode URLs
    Tenant("my-tenant").
    AgentName("my-tool").
    APIKey(os.Getenv("CX_API_KEY")).  // or .OAuth(id, secret)
    Build()
if err != nil { log.Fatal(err) }
defer client.Close()  // ALWAYS close — releases connection pool
```

Required fields: Region, Tenant, AgentName, one of APIKey/OAuth. Client is goroutine-safe.

### Context
Every SDK method takes `context.Context` first. Use `context.WithTimeout` for per-call deadlines.

### Error Handling
Use `errors.As` with the SDK error catalog:
- `*cxerrors.ConfigurationError` — invalid config
- `*cxerrors.AuthError` — auth failed
- `*cxerrors.CommunicationError` — retries exhausted
- `*cxerrors.ResponseError` — bad status/body
- `*cxerrors.ScanError` — scan failure
- `*cxerrors.ReportError` — report failure

### Auth & Retry
- Auth is lazy (first call), 401 auto-refreshes transparently
- SDK retries 3x on 5xx/transient errors with randomized backoff
- Do NOT add your own retry logic

### Workflows
- **Builders**: `client.Scans().NewScan(repo).ForBranch("main").WithEngines("sast").Start(ctx)`
- **Inspectors**: `insp.Executing()`, `insp.Successful()`, `insp.Failed()`
- **Waiters**: `scans.WaitUntilComplete(ctx, insp, opts)` — polls with cancellation support
- **Readers**: `client.Presets()`, `client.Users().NewReader()` — lazy-loaded cached catalogs
- **GetOrCreate**: `client.Users().GetOrCreateByEmail(ctx, template)`, `client.Projects().GetOrCreateByName(ctx, template)`

### Models
Import from `github.com/cx-andrew-gehring/cxone-sdk-golang/models`. All types are strongly-typed structs. Unknown response fields are tolerated for forward compatibility.

### Observability
Wire via builder: `.Logger(l).Metrics(m).Tracer(t)`. Defaults are no-ops. Structured events emitted automatically.

## Anti-Patterns — Never Do These
- Create a new client per request (reuse one)
- Add retry wrappers around SDK calls (built in)
- Parse raw HTTP responses (use typed models)
- Hardcode Checkmarx One URLs (use Region constants)
- Log credentials or bearer tokens (SDK redacts automatically)
- Use `context.TODO()` in production
- Ignore `client.Close()` errors
