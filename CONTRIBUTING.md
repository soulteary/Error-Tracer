# Contributing to Error-Tracer

[简体中文](CONTRIBUTING.zh-CN.md)

Thank you for helping improve Error-Tracer. Keep pull requests focused on one
reviewable concern and include tests for behavior changes.

## Development setup

Use the Go version declared in `go.mod`. Bun 1.4.0 or newer is required for the
browser SDK tests and documentation checks.

```sh
go mod verify
go vet ./...
go test ./...
go test -race ./...
bun run test
bun run check:docs
```

Run benchmarks and the HTTP load test only with the bounded commands in
[the performance guide](docs/performance.md). Never point the load test at a
system unless you are authorized to generate that traffic.

## Pull requests

- Format Go files with `gofmt` and keep JavaScript dependency-free unless a
  dependency has a clear operational benefit.
- Preserve atomicity, project isolation, input bounds, and credential handling
  when changing ingestion or storage code.
- Update both English and Simplified Chinese user documentation when a public
  command, option, endpoint, or dashboard string changes.
- Do not commit SQLite databases, credentials, generated binaries, coverage
  files, or load-test output.
- Describe compatibility changes and list the exact verification commands in
  the pull request.

Report security problems using [the security policy](SECURITY.md), not a public
issue containing exploit details.
