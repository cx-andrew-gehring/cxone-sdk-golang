# Changelog

## Unreleased

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
- pagination: iterators now stop on a short page and, where the endpoint
  reports it, once `totalCount` has been consumed (accounting for Pages vs
  Items offset mode). Previously `Results().Stream`, `StreamSAST` and the raw
  iterators looped forever against a server that always returned a full page.
  New safeguards: `pagination.ErrPaginationStalled` (two identical consecutive
  pages) and `pagination.Config.MaxPages` (default 100000,
  `pagination.ErrMaxPagesExceeded`). `/api/results` defaults to page-count
  offsets and `/api/sast-results` to item-count offsets, per the OpenAPI specs.
- uploads: presigned upload URLs (signature query, userinfo) no longer appear
  in `CommunicationError` messages, fields or wrapped transport errors.
- projects: `GetOrCreateByName` re-runs the exact-name lookup once when create
  fails with a 4xx conflict (lost a create race).

### Changed (behaviour / breaking)
- uploads: the presigned-URL PUT (`PutFile`, `PutFileAuth`, and the scan
  invoker without `WithUploadTimeout`) now always has a deadline:
  `httpClient.Timeout` when > 0, otherwise a two-hour ceiling (previously a
  client with `Timeout == 0` meant no limit). `PutFileWithTimeout` /
  `PutFileAuthWithTimeout` / `WithUploadTimeout(d)` set a different deadline,
  e.g. for very large archives. Deadline errors are `CommunicationError`s.
- Fewer requests: iterators no longer issue a trailing empty-page request after
  a short final page or a page that reaches `totalCount`.
- `ScanInvoker.WithTags` no longer sets `project.tags`; it sets the scan's own
  `tags`. Use the new `WithProjectTags` to set project tags (note: the API
  overwrites the project's existing tags). `project.tags` is omitted from the
  scan request unless `WithProjectTags` is called.
- `WithEngines` validates names against the spec config-type enum (`sast`,
  `sca`, `kics`, `apisec`, `containers`, `microengines`, `aisc`); unknown or
  duplicate names return a `ConfigurationError` before any HTTP call. `scs` is
  accepted as a deprecated alias for `microengines`.
- `WithConfigs` (raw `config[]` passthrough) now satisfies the "at least one
  engine" requirement on its own; its types are validated like `WithEngines`
  and must not duplicate an engine already selected.

### Added
- internal API: `uploads.PutFileAuth`, `uploads.PutFileAuthWithTimeout`,
  `uploads.Auth`, `transport.Executor.Authenticator()`. Existing signatures
  are unchanged.
- `WithProjectTags`, `WithUploadTimeout`, `WithConfigs` on the scan invoker;
  raw result iterators (`Advanced().Results().IterRaw` / `IterSASTRaw`),
  `Results().Stream` / `StreamSAST`; `pagination.NewWithOptions`.
