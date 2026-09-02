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

### Removed

- The PHP/MySQL runtime is no longer part of the active branch. It remains
  available from the `v1.0.0-legacy` tag for historical reference.

[2.0.0]: https://github.com/soulteary/Error-Tracer/releases/tag/v2.0.0
