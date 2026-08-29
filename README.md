# Error-Tracer

[简体中文](README.zh-CN.md)

Error-Tracer is a small, self-hosted browser error collector. The current
service is written in Go, stores aggregated issues in SQLite, ships a
dependency-free browser SDK, and includes an embedded triage dashboard.

The original 2013 PHP/MySQL implementation is preserved in the
[`v1.0.0-legacy`](https://github.com/soulteary/Error-Tracer/tree/v1.0.0-legacy)
tag. It is not part of the current runtime.

## Features

- Captures browser errors, unhandled promise rejections, and failed resources.
- Normalizes events and removes URL credentials, queries, and fragments before
  persistence.
- Groups matching events with a stable SHA-256 fingerprint.
- Persists issue aggregates in SQLite.
- Writes batches of up to 100 events in one transaction and rolls the entire
  batch back on failure.
- Tracks `open`, `resolved`, and `ignored` issue states.
- Provides an authenticated JSON API and an embedded dashboard.
- Offers English and Simplified Chinese dashboard locales without browser
  storage.
- Includes an opt-in, read-only demo backed only by built-in in-memory data.
- Enforces request-size limits, exact browser-origin allowlists, per-peer rate
  limits, and constant-time credential comparisons.
- Runs as a single static binary or a non-root, read-only container.
- Ships database/application benchmarks and a bounded HTTP load-test command.

## Quick start with Docker Compose

Copy the environment template:

```sh
cp .env.example .env
```

Generate two independent credentials and put them in `.env`:

```sh
openssl rand -hex 16
openssl rand -hex 24
```

The first value is suitable for `ERROR_TRACER_INGEST_KEY`; the second is
suitable for `ERROR_TRACER_ADMIN_TOKEN`. Set
`ERROR_TRACER_ALLOWED_ORIGINS` to the exact origin of every browser application
that will submit events, for example `https://app.example.com`.

Start the service:

```sh
docker compose up --build -d
curl --fail http://localhost:8080/readyz
```

Open <http://localhost:8080/> and connect with the admin token. The dashboard
keeps the token only in the current tab's memory.

Use the language selector or open `/?lang=zh-CN` for Simplified Chinese. The
selection is reflected in the URL and is not saved to browser storage.

The Compose deployment stores `error-tracer.db` in the named
`error-tracer-data` volume.

To evaluate the dashboard before ingesting real events, enable the isolated
read-only demo described in [Demo mode](docs/demo.md).

## Browser SDK

The service exposes its embedded SDK at `/assets/error-tracer.js`:

```html
<script src="https://errors.example.com/assets/error-tracer.js"></script>
<script>
  const tracer = ErrorTracer.init({
    endpoint: "https://errors.example.com/api/v1/events",
    projectKey: "replace-with-the-ingest-key",
    release: "web@2026.08.29",
    environment: "production",
    tags: { region: "ap-southeast-1" },
  });

  tracer.captureMessage("checkout started");
</script>
```

Automatic capture is enabled by default. Set `autoCapture: false` when only
manual capture is wanted. The client also supports `sampleRate`,
`maxEventsPerMinute`, `beforeSend`, and a custom `transport`.

## Ingestion API

### One event

`POST /api/v1/events` accepts JSON or `text/plain` JSON, which allows the SDK
to use `navigator.sendBeacon`:

```json
{
  "project_key": "replace-with-the-ingest-key",
  "event": {
    "kind": "error",
    "message": "TypeError: value is not a function",
    "stack": "TypeError: value is not a function\n    at checkout (app.js:10:2)",
    "source_url": "https://app.example.com/app.js?build=42",
    "page_url": "https://app.example.com/checkout?session=secret",
    "line": 10,
    "column": 2,
    "occurred_at": "2026-08-29T14:00:00Z",
    "release": "web@2026.08.29",
    "environment": "production",
    "tags": {
      "feature": "checkout"
    }
  }
}
```

The server overwrites `id`, `received_at`, and `user_agent`. A successful
request returns HTTP `202 Accepted`:

```json
{
  "id": "evt_0123456789abcdef0123456789abcdef",
  "fingerprint": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
```

### Atomic batch

`POST /api/v1/events/batch` accepts between 1 and 100 events. The decoded body
is limited to 1 MiB. Every event is normalized and validated before SQLite is
modified; all UPSERTs then run in one transaction.

```json
{
  "project_key": "replace-with-the-ingest-key",
  "events": [
    {
      "kind": "error",
      "message": "first failure"
    },
    {
      "kind": "unhandled_rejection",
      "message": "second failure"
    }
  ]
}
```

The response contains one server-assigned ID and fingerprint per input event:

```json
{
  "events": [
    {
      "id": "evt_11111111111111111111111111111111",
      "fingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    },
    {
      "id": "evt_22222222222222222222222222222222",
      "fingerprint": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    }
  ]
}
```

If validation, an UPSERT, a readback, or the commit fails, no part of that
batch is persisted.

## Issue API

Issue endpoints require the configured admin token:

```http
Authorization: Bearer replace-with-the-admin-token
```

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/issues?limit=50&offset=0` | List issues, newest first |
| `GET` | `/api/v1/issues?status=open` | Filter by `open`, `resolved`, or `ignored` |
| `GET` | `/api/v1/issues/{fingerprint}` | Read one issue |
| `PATCH` | `/api/v1/issues/{fingerprint}` | Change the issue status |

Status update body:

```json
{
  "status": "resolved"
}
```

Pages are limited to 100 issues. Offsets above 100,000 are rejected.

## Service endpoints

| Method | Path | Authentication | Description |
| --- | --- | --- | --- |
| `GET` | `/` | None | Dashboard shell; live data calls require an admin token |
| `GET` | `/assets/error-tracer.js` | None | Embedded browser SDK |
| `GET` | `/api/v1/meta` | None | Public feature metadata; currently only the demo flag |
| `GET` | `/api/v1/demo/issues` | None, demo mode only | List built-in read-only demo issues |
| `GET` | `/api/v1/demo/issues/{fingerprint}` | None, demo mode only | Read one built-in demo issue |
| `POST` | `/api/v1/events` | Ingest key in the body | Submit one event |
| `POST` | `/api/v1/events/batch` | Ingest key in the body | Submit an atomic batch |
| `GET` | `/api/v1/issues` | Admin bearer token | List issues |
| `GET` | `/api/v1/issues/{fingerprint}` | Admin bearer token | Read an issue |
| `PATCH` | `/api/v1/issues/{fingerprint}` | Admin bearer token | Update status |
| `GET` | `/healthz` | None | Process liveness |
| `GET` | `/readyz` | None | Readiness for new work |

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `ERROR_TRACER_ADDRESS` | No | `:8080` | HTTP listen address |
| `ERROR_TRACER_DATABASE_PATH` | No | `error-tracer.db` | SQLite database path |
| `ERROR_TRACER_PROJECT_ID` | No | `default` | Project namespace owned by this process |
| `ERROR_TRACER_INGEST_KEY` | Yes | — | Ingestion credential, at least 16 bytes |
| `ERROR_TRACER_ADMIN_TOKEN` | Yes | — | Admin credential, at least 24 bytes |
| `ERROR_TRACER_ALLOWED_ORIGINS` | No | empty | Comma-separated exact HTTP(S) browser origins |
| `ERROR_TRACER_RATE_PER_MINUTE` | No | `120` | Ingestion requests per minute per direct peer |
| `ERROR_TRACER_RATE_BURST` | No | `30` | Maximum token-bucket burst per direct peer |
| `ERROR_TRACER_DEMO_MODE` | No | `false` | Expose the isolated, public, read-only demo |

`ERROR_TRACER_PORT` is a Compose-only host-port setting and defaults to `8080`.
An empty origin allowlist disables browser-origin ingestion while still
allowing clients that do not send an `Origin` header.

## Local development

The module declares Go 1.27. Node.js 22 or newer is needed only for browser SDK
tests.

```sh
go mod verify
go vet ./...
go test ./...
go test -race ./...
npm test
```

Performance and capacity checks are opt-in. See
[Performance and load testing](docs/performance.md) for reproducible commands,
metric definitions, and load-test safety controls.

Run the service from source:

```sh
ERROR_TRACER_INGEST_KEY=development-key-1 \
ERROR_TRACER_ADMIN_TOKEN=development-admin-token-1 \
go run ./cmd/error-tracer
```

Build the static service binary:

```sh
CGO_ENABLED=0 go build -trimpath -o error-tracer ./cmd/error-tracer
```

## Documentation

- [Demo mode and its security boundary](docs/demo.md)
- [Performance benchmarks and load testing](docs/performance.md)
- [Simplified Chinese README](README.zh-CN.md)

## Deployment notes

- Use independent, randomly generated ingest and admin credentials.
- Put the service behind HTTPS before accepting browser traffic over a
  network.
- Configure exact browser origins; wildcards are intentionally rejected.
- The rate limiter uses the direct TCP peer and does not trust forwarded
  address headers.
- Persist the SQLite path or `/data` volume outside the container lifecycle.
- The process marks itself unready before graceful HTTP shutdown.

## License

Error-Tracer is licensed under the [Apache License 2.0](LICENSE).
