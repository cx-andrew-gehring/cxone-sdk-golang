# Changelog

## v0.12.0 (2026-10-26)

### Added
- `Advanced().Credits()` with read access to two credits endpoints that
  Checkmarx does not document (shapes observed live):
  - `Consumption(ctx, models.CreditConsumptionQuery)` returns one typed page of
    `GET /api/v1/credits/consumption` (`models.CreditConsumptionPage`).
  - `ConsumptionAll(ctx, query)` walks every page (driven by `totalPages`) and
    returns all items; on any page error it returns `nil` and the error, never
    a partial list.
  - `ActionRates(ctx)` returns the raw JSON body of
    `GET /api/v1/credits/action-rates` (`json.RawMessage`, shape unconfirmed).
- `models.CreditConsumptionItem.CreditsUsed` is a decimal string and is never
  converted to a float. `ActionsPerformed` is `map[string]json.Number`.
  Unknown response fields are ignored.

### Notes
- The paging query parameters are not confirmed. They default to `page` and
  `limit` (first page `1`) and can be overridden with
  `CreditConsumptionQuery.PageParam`, `LimitParam` and `FirstPage`.
  `ConsumptionAll` fails if a response's `currentPage` differs from the page
  requested, so a wrong guess cannot silently skip pages.

## v0.11.0 (2026-10-26)

### Added
- `models.AiConfig` (with `Config()`) builds the typed `ai` config entry. The
  scan Invoker accepts config type `ai`.
- `Invoker.WithFusion(model)` enables Checkmarx Fusion for the scan with the
  given LLM model. It appends `{"type":"ai","value":{"fusionEnabled":"true","llmModel":"<model>"}}`.
- Invoker rule: an `ai` entry with `fusionEnabled:"true"` requires `llmModel`.
  Without it the call returns a `ConfigurationError` before any HTTP call.
- `cxerrors.InvalidLLMError` (alias `cxone.InvalidLLMError`) is returned when
  CxOne answers `POST /api/scans` with HTTP 400 and error code 209.
- `ResponseError.ErrorCode` and `ResponseError.ServerMessage` are filled from
  the JSON error body (`code` and `message`).

### Notes
- The Fusion model is chosen only through the separate `ai` config entry, for
  example `{"type":"ai","value":{"fusionEnabled":"true","llmModel":"haiku-4.5"}}`.
  It is NOT chosen through `pansophiaEnabled` or `llmModel` on the `sast` entry.
- Known models today are `haiku-4.5` and `opus-4.7`. The SDK does not validate
  model names. CxOne rejects an unknown name with error code 209
  (`InvalidLLMError`).
- When no `ai` entry is sent, CxOne enables Fusion itself with its own default model.

## v0.10.1 (2026-10-23)

### Changed (breaking: import path)
- The module path is now `github.com/cx-andrew-gehring/cxone-sdk-golang`, the
  repository it is published from (it was `github.com/checkmarx-open-labs/cxone-sdk-golang`,
  which is not reachable). Update imports and `go get github.com/cx-andrew-gehring/cxone-sdk-golang@v0.10.1`;
  a `replace` directive is no longer needed. The API is unchanged from v0.10.0.
- `Version` is `0.10.1` (v0.10.0 still reported `0.9.0`, which also appeared in the
  `User-Agent`).
- The template project and its setup scripts use the new path and owner.

## v0.10.0 (2026-10-23)

Changes since `00d52c7`. v0.10.0 still declared the old module path; use v0.10.1.

### Fixed
- uploads: the presigned-URL PUT now sends `Authorization: Bearer <token>`
  when, and only when, the URL's origin (scheme + host + port) is exactly the
  client's API origin (tenants whose storage is fronted by the API host, e.g.
  `https://deu.ast.checkmarx.net/storage/uploads...`, answered 401 before).
  URLs on any other host (S3/MinIO) still get no Authorization header. The
  match is fail-closed (userinfo, trailing-dot or non-ASCII hosts, other
  ports/schemes never match). With the token attached, redirects are not
  followed (a 3xx surfaces as a `CommunicationError`), and a 401 triggers one
  token refresh and replay when the body is an `io.Seeker`. Used by
  `Advanced().Uploads().PutFile`/`UploadFromReader` and `ScanInvoker`
  upload mode.
- uploads: presigned upload URLs (signature query, userinfo) no longer appear
  in `CommunicationError` messages, fields or wrapped transport errors.
- projects: `GetOrCreateByName` looks the project up with the exact-match
  `names=` filter (previously `name=`, a substring match capped at 10 rows
  that could miss the project and cause a duplicate create).
- projects: when the create fails with a 4xx (lost a create race),
  `GetOrCreateByName` re-runs the exact-name lookup. For the measured
  duplicate-name rejection (HTTP 400, `code` 208, message `... already
  exists`; 409 is accepted too) it retries the lookup up to 3 times with a
  short ctx-aware backoff (0.5 s, 1 s, 2 s) and, if the project is still not
  visible, returns the new typed `*cxerrors.ConflictError` (alias
  `cxone.ConflictError`, wraps the original `*ResponseError`). Other 4xx get
  one re-lookup and then the original error, as before.
- aitriage: `CONFIRMED` (returned by the live service, not in the spec enum)
  is now a known terminal status: `Done()` is true and `Unknown()` false.

### Changed (behaviour / breaking)
- pagination: ALL iterators built on `pagination.Iterator` (every typed and
  raw `Iter*`, `Collect`, `ListAll`, `Results().Stream`/`StreamSAST`, and the
  high-level listings built on them) now stop on a page shorter than the
  requested page size and, where the endpoint reports it, once `totalCount`
  has been consumed (accounting for Pages vs Items offset mode). Previously
  they stopped only on an empty page, so a server that always returned a
  full page looped forever, and every walk ended with one extra empty-page
  request. Consequence: a service that caps the page size below the
  requested size ends the walk after its first page; use
  `Iterator.Total()` / `Results().StreamWithStats` to detect that.
- pagination: new safeguards on every iterator: `pagination.ErrPaginationStalled`
  (two identical consecutive pages, when the iterator has an identity key)
  and `pagination.Config.MaxPages` (default 100000,
  `pagination.ErrMaxPagesExceeded`).
- results: `/api/results` raw iterators default to page-count offsets and
  `/api/sast-results` to item-count offsets, per the OpenAPI specs (the
  typed `/api/results` iterator still advances by items).
- uploads: the presigned-URL PUT (`PutFile`, `PutFileAuth`, and the scan
  invoker without `WithUploadTimeout`) now always has a deadline:
  `httpClient.Timeout` when > 0, otherwise a two-hour ceiling (previously a
  client with `Timeout == 0` meant no limit). `PutFileWithTimeout` /
  `PutFileAuthWithTimeout` / `WithUploadTimeout(d)` set a different deadline,
  e.g. for very large archives. Deadline errors are `CommunicationError`s.
- scans: `ScanInvoker.WithTags` no longer sets `project.tags`; it sets the
  scan's own `tags`. Use the new `WithProjectTags` to set project tags (the
  API overwrites the project's existing tags). `ScanProject.Tags` is now
  `omitempty`, so `project.tags` is omitted from the scan request unless
  `WithProjectTags` is called.
- scans: the scan handler wire format omits an empty `repoUrl` and a zero
  `credentials` object (upload scans no longer send `"repoUrl":""` and empty
  credentials); `uploadFormat` is sent when set.
- scans: `WithEngines` validates names against the spec config-type enum
  (`sast`, `sca`, `kics`, `apisec`, `containers`, `microengines`, `aisc`);
  unknown or duplicate names return a `ConfigurationError` before any HTTP
  call. `scs` is accepted as a deprecated alias for `microengines`.
- scans: `WithConfigs` (raw `config[]` passthrough) satisfies the "at least
  one engine" requirement on its own; its types are validated like
  `WithEngines` and must not duplicate an engine already selected.

### Added
- `Scans().ListForProject(ctx, projectID)`;
  `Projects().Delete(ctx, id, cancelRunning)` (cancels Queued and Running
  scans first when asked, already-gone is success);
  `Projects().IterByNameRegex(pattern)`.
- AI triage: `Client.AITriage()` with `Submit` and `Result`
  (`workflows/aitriage.ResultInspector`: `Status`, `Done`, `Failed`,
  `Unknown`, `Detail`, `RawDetail`), `Advanced().AITriage()`, models
  `TriageRequest`, `TriageBucket`, `TriageAccepted`, `TriageResultEnvelope`
  (now with `RawDetail`), `TriageResultDetail`; typed `*cxerrors.TriageError`
  for 400/402/403. Note: `Detail()` is a typed projection: re-encoding it
  drops unknown fields and (`omitempty`) false/empty values; use
  `RawDetail()` for the body exactly as received.
- Risks: `Client.Risks().GroupIDs(ctx, projectID, engines...)`,
  `Advanced().Risks().ListRisks` / `IterRisks`.
- `WithProjectTags`, `WithUploadTimeout`, `WithConfigs` on the scan invoker;
  raw result iterators (`Advanced().Results().IterRaw` / `IterSASTRaw`),
  `Results().WithPagination`, `Results().Stream` / `StreamSAST`;
  `pagination.NewWithOptions`, `pagination.Options`, `pagination.OffsetMode`.
- `Iterator.Total()` (the server's totalCount as of the last page) and
  `Results().StreamWithStats` / `StreamSASTWithStats` returning
  `pagination.StreamStats{Delivered, Total, TotalKnown}` with `Truncated()`.
- `RetryAfter time.Duration` on `ResponseError` and `CommunicationError`:
  the parsed `Retry-After` header (seconds or HTTP-date) of the final
  response, 0 when absent. By default HTTP 429 is still NOT retried by the
  request funnel (CLAUDE.md §4.4); callers that retry use this value.
- `ClientBuilder.RetryOnTooManyRequests(maxWait time.Duration)`: an opt-in,
  OFF by default (default behaviour is unchanged: a 429 is returned at once
  with `RetryAfter`). With `maxWait > 0` the funnel retries a 429 within the
  existing `Retries` attempt budget, waiting the server's `Retry-After`
  (seconds or HTTP-date) or, when absent, the normal jittered backoff. The
  429 is returned unchanged instead when `Retry-After` exceeds `maxWait`,
  when it would outlast the call's deadline, or on the last attempt. Waits
  are cancelled with the context. Every method is retried, POST included:
  a 429 means the server did not process the request, so a re-send cannot
  duplicate its effect. Each retry emits the `request_retry` log event and
  `Metrics.IncRetry("status_429")`. Negative `maxWait` is a
  `ConfigurationError`. Documented in CLAUDE.md §4.4 as an optional extension.
- `Client.IsAPIOrigin(url)`: the same-origin rule the upload PUT uses for the
  bearer token, so callers can tell an API-origin upload failure from an
  object-store one.
- internal API: `uploads.PutFileAuth`, `uploads.PutFileAuthWithTimeout`,
  `uploads.Auth`, `uploads.SameOrigin`, `transport.Executor.Authenticator()`,
  `retry.ParseRetryAfter`, `retry.WithDelay`, `retry.UnknownOutcomeError`,
  `transport.Config.RateLimitMaxWait`. Existing signatures are unchanged.
