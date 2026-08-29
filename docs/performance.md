# Performance and load testing

Error-Tracer includes repeatable Go benchmarks for storage and HTTP processing,
plus a dependency-free command for end-to-end pressure tests. Benchmarks are not
part of the normal unit-test run and must be invoked explicitly.

Run tests and vet before collecting performance data:

```sh
go test ./...
go vet ./...
```

## Runtime metrics

Enable the dependency-free Prometheus endpoint in a trusted environment:

```dotenv
ERROR_TRACER_METRICS_ENABLED=true
```

Then scrape `GET /metrics`. The endpoint reports bounded route templates rather
than raw request paths, HTTP status counts, request-duration histograms,
in-flight requests, committed event count, readiness, and demo-mode state. The
metrics contain no project ID, credential, URL, fingerprint, message, or other
event-controlled label.

The endpoint is unauthenticated when enabled. Restrict it with the reverse
proxy, container network, or firewall instead of exposing it to the public
internet. A minimal Prometheus scrape job is:

```yaml
scrape_configs:
  - job_name: error-tracer
    static_configs:
      - targets: ["error-tracer:8080"]
```

## Storage benchmarks

The storage suite compares the concurrency-safe in-memory implementation with
SQLite. It measures atomic batches of 1, 10, and 100 events and 50-row offset
and cursor page reads from 1,000 issues:

```sh
go test ./internal/store \
  -run '^$' \
  -bench 'Benchmark(RecordBatch|ListIssues)$' \
  -benchmem \
  -count 5
```

SQLite benchmarks create temporary databases and exclude database creation and
fixture setup from the timed section. Batch inputs reuse a bounded set of
fingerprints, so the write benchmark measures transaction, UPSERT, and readback
cost rather than unbounded database growth.

## HTTP handler benchmarks

The application benchmark exercises the complete in-process HTTP handler,
including JSON decoding, authentication, normalization, validation, storage,
and JSON response encoding. It covers the single-event route and batches of 10
and 100 events:

```sh
go test ./internal/server \
  -run '^$' \
  -bench BenchmarkIngestHandler \
  -benchmem \
  -count 5
```

Use `benchstat` or another statistical comparison tool when evaluating changes;
do not compare single runs from different machines.

## End-to-end pressure test

Start an isolated Error-Tracer instance with a disposable SQLite database. The
default rate limit is intentionally low for a pressure test, so raise it only in
that isolated environment:

```sh
ERROR_TRACER_DATABASE_PATH=/tmp/error-tracer-loadtest.db \
ERROR_TRACER_PROJECT_ID=loadtest \
ERROR_TRACER_INGEST_KEY=replace-with-a-16-byte-key \
ERROR_TRACER_ADMIN_TOKEN=replace-with-a-24-byte-token \
ERROR_TRACER_RATE_PER_MINUTE=60000 \
ERROR_TRACER_RATE_BURST=10000 \
go run ./cmd/error-tracer
```

In another terminal, run a bounded test against the local batch endpoint:

```sh
ERROR_TRACER_INGEST_KEY=replace-with-a-16-byte-key \
go run ./cmd/error-tracer-loadtest \
  -target http://127.0.0.1:8080 \
  -duration 30s \
  -concurrency 16 \
  -batch-size 50 \
  -cardinality 1000 \
  -rate 500
```

`-target` accepts the service base URL or the complete
`/api/v1/events/batch` URL. The summary reports:

- total requests and requests per second;
- accepted requests, accepted events, and events per second;
- HTTP status counts and transport-error count;
- approximate p50, p95, p99, and maximum response latency.

The default `-fail-on-error=true` makes the command exit non-zero if no request
is accepted, a transport error occurs, or any response is not `202 Accepted`.
Set `-fail-on-error=false` only when non-202 responses are part of the test.

## Safety controls

The load-test command is intentionally conservative:

- A target is required; there is no implicit server address.
- Non-loopback targets are refused unless `-allow-remote` is explicit.
- Redirects are not followed and environment HTTP proxies are not used.
- Duration is bounded to 1 second–30 minutes.
- Concurrency is bounded to 1–1,000 workers.
- Batch size is bounded to the server limit of 1–100 events.
- Fingerprint cardinality is bounded to 1–100,000 distinct issues.
- Optional request rate is bounded to 1,000,000 requests per second.
- Per-request timeout is bounded to 100 milliseconds–1 minute.
- The ingest key can come from `ERROR_TRACER_INGEST_KEY` and is never printed.

Only use `-allow-remote` for infrastructure you own or are explicitly authorized
to test. Coordinate the test window, retention plan, and alert suppression with
the service owner before targeting a shared environment.

## Interpreting results

Increase one dimension at a time: request rate, concurrency, batch size, or
fingerprint cardinality. Watch the service's CPU, memory, filesystem latency,
database size, `429` responses, and tail latency. A useful capacity point keeps
the accepted event rate stable without sustained error growth or unbounded p99
latency.

The application uses one SQLite connection to serialize writes. Large batches
reduce transaction overhead, while high fingerprint cardinality increases
database size and index work. Test both aggregation-heavy and high-cardinality
profiles before choosing production limits.
