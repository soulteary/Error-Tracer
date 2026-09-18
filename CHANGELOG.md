# Changelog

Notable changes to Error-Tracer are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## [2.0.0] - Unreleased

### Breaking

- `ERROR_TRACER_RETENTION_DAYS` now defaults to `90` instead of `0`. A
  deployment that never configured retention kept every issue forever; after
  upgrading, the first sweep **permanently deletes issues whose last
  occurrence is older than 90 days**, along with their retained event history.
  Set `ERROR_TRACER_RETENTION_DAYS=0` before upgrading to keep the previous
  behaviour, or set it to the window you actually want. Back up first with
  `error-tracer db backup`.

### Added

- A self-contained Go service with SQLite persistence, an authenticated issue
  API, and an embedded dashboard.
- Atomic batch ingestion, bounded pagination and occurrence history, retention
  controls, health probes, Prometheus metrics, and SQLite maintenance commands.
- A dependency-free browser SDK with automatic capture, privacy hooks, bounded
  batching, retry limits, and delivery counters.
- English and Simplified Chinese documentation and dashboard interfaces.
- An isolated read-only demo command with realistic in-memory samples.
- `ERROR_TRACER_MAX_ISSUES` caps issues per project, evicting the least
  recently seen first and cascading their event history. Retention bounds how
  long data is kept; this bounds how much there is, which a reporter drives
  because a fingerprint includes the client-supplied message. Swept every five
  minutes so it reacts to ingestion rather than to the clock.
- `ERROR_TRACER_SDK_CORS_ENABLED` serves the browser SDK with
  `Access-Control-Allow-Origin`, which a page needs before it can pin the
  bundle with Subresource Integrity against the `ETag` already served.
- A startup warning when neither retention nor the issue cap is configured.
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
- `getStats()` reports four additional counters — `sampled`, `suppressed`,
  `throttled`, and `invalid` — so events the SDK discards on purpose are
  visible instead of silent. `dropped` continues to mean an event was lost.

### Fixed

- Startup no longer ignores `SIGTERM`. Opening the database runs the migrations
  and the event-history reconcile under a background context, so a rolling
  update had to wait out the whole startup before the container would stop.
- A paginated response whose cursor could not be encoded dropped
  `next_cursor` entirely, which a client reads as the end of the walk. The
  invariant violation is now logged and reported as `500 internal_error`.
- Issue and health responses carry `X-Content-Type-Options: nosniff`, which the
  asset, dashboard, and metrics responses already set.
- The first retention sweep no longer runs before the listener opens, so
  startup is not delayed in proportion to the expired-issue backlog.
- The admin availability guard accepts any configured token instead of only the
  current one, so a caller that supplies only a previous token is authorized
  rather than told the API is unavailable. Empty slots are excluded from the
  candidate list, and readiness uses the same rule.
- Two dashboard text colours fell below the WCAG AA 4.5:1 contrast minimum at
  the 10-12px sizes they were used at: `--subtle` reached 3.90:1 against the
  panel surface and the admin-token placeholder only 2.45:1.
- The dashboard had no `banner` landmark, because the masthead — brand,
  version, language selector and connection state — sat inside `<main>`.
- Changing the status filter or stepping a page rewrote the result summary and
  the page indicator without announcing either to assistive technology.
- The result summary could render an inverted range such as
  "Showing 51-12 of 12" when retention pruning shrank the total during a
  cursor walk.
- The browser SDK stopped capturing entirely after a backward clock step — an
  NTP correction, a VM resume — because every stored rate-window stamp sat in
  the future where the 60-second cutoff could not reach it.
- `destroy()` is a hard stop. It still flushes what is queued, but a later
  capture no longer re-queues, re-arms the flush timer, or transmits.
- The SDK's flush timer calls are guarded, so a page that patches `setTimeout`
  or `clearTimeout` to throw cannot throw out of `captureMessage`.
- An oversized `User-Agent` request header rejected an otherwise valid event
  with `422 invalid_event` naming `user_agent` — a field the collector assigns
  and the reporting client cannot shorten. It is now truncated to the ingest
  limit on a rune boundary.
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
