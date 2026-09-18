# Changelog

Notable changes to Error-Tracer are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## [2.0.0] - Unreleased

### Added

- A self-contained Go service with SQLite persistence, an authenticated issue
  API, and an embedded dashboard.
- Atomic batch ingestion, bounded pagination and occurrence history, retention
  controls, health probes, Prometheus metrics, and SQLite maintenance commands.
- A dependency-free browser SDK with automatic capture, privacy hooks, bounded
  batching, retry limits, and delivery counters.
- English and Simplified Chinese documentation and dashboard interfaces.
- An isolated read-only demo command with realistic in-memory samples.
- Database, application, and HTTP load tests with explicit safety limits.
- Reproducible multi-platform release archives, checksums, SPDX SBOMs,
  provenance attestations, and multi-architecture container images.

### Changed

- Replaced the repository-local release Bash script with the SHA-pinned
  `ci-recipes` Go CLI shared by CI and the documented local release process.
- SQLite schema changes use ordered, transactional migrations.
- A resolved issue returns to `open` when the same failure recurs; ignored
  issues remain ignored.
- Production containers run as a non-root user with a read-only-compatible
  filesystem layout.
- Stack traces are scrubbed of URL credentials, queries, and fragments on both
  the collector and the browser SDK, matching `source_url` and `page_url`.
  Issue fingerprints are unchanged.
- The pre-parse request bucket is charged before the browser-origin check, so
  origin-rejected traffic is bounded too. `OPTIONS` preflights stay exempt
  because a browser must preflight before it can POST.
- `ERROR_TRACER_ALLOWED_ORIGINS` rejects origins that can never match a browser
  `Origin` header — wildcards, empty hosts, non-ASCII host names, and ports
  outside 1-65535 — instead of accepting them and silently refusing every
  browser request.
- The browser SDK's `maxBatchBytes` defaults to 256 KiB rather than the 60 KiB
  keepalive budget, so a full-size stack trace is no longer dropped before it
  reaches the transport. The transport still selects Beacon or fetch per
  payload.
- Compose publishes the host port on `127.0.0.1` by default through the new
  Compose-only `ERROR_TRACER_BIND` setting.
- The in-memory store clones only the issues it returns rather than every match
  in the project, cutting a 50-row page over 1,000 issues from 2,016 to 117
  allocations.

### Fixed

- A stack whose first line contained `@` and ended in `:<digits>` — an ordinary
  message line, not a frame — was taken for a SpiderMonkey frame, so every
  error sharing that line grouped into one issue regardless of its real call
  site.
- The browser SDK derived `<origin>/batch` from a bare-origin `endpoint`, a
  route the collector does not register, so every batch failed and exhausted
  the retry budget. A custom path is still preserved for proxy prefixes.
- Prometheus label values were escaped twice, exposing a quoted value as
  `a\"b` rather than `a"b`.
- `error-tracer db check` and `error-tracer db backup` failed with
  `invalid uri authority` whenever the database path or the backup destination
  was relative, which includes the default `ERROR_TRACER_DATABASE_PATH`.
- A subcommand given extra arguments fell through to the serve path and opened
  the configured database read-write instead of reporting a usage error.
- Ingestion failures from the store are logged instead of being discarded
  behind an opaque `500 internal_error`.
- An unset `received_at` is omitted from JSON rather than serialized as
  `0001-01-01T00:00:00Z`.
- The dashboard declares the monospace custom property it referenced, so the
  build-version chip renders in the intended typeface.

### Removed

- The PHP/MySQL runtime is no longer part of the active branch. It remains
  available from the `v1.0.0-legacy` tag for historical reference.

[2.0.0]: https://github.com/soulteary/Error-Tracer/releases/tag/v2.0.0
