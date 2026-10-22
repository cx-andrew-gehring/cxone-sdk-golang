# Changelog

## Unreleased

### Fixed
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
- `WithProjectTags`, `WithUploadTimeout`, `WithConfigs` on the scan invoker;
  raw result iterators (`Advanced().Results().IterRaw` / `IterSASTRaw`),
  `Results().Stream` / `StreamSAST`; `pagination.NewWithOptions`.
