# Demo mode

Demo mode makes the embedded dashboard useful before a project has submitted
any real events. It is disabled by default and must be enabled explicitly:

```dotenv
ERROR_TRACER_DEMO_MODE=true
```

With Docker Compose, add that value to `.env`, start the service, and open the
dashboard:

```sh
docker compose up --build -d
curl --fail http://localhost:8080/readyz
```

The sign-in panel then offers **Explore the read-only demo**. The demo includes
five realistic issues across `open`, `resolved`, and `ignored` states, with 106
sample occurrences in total. Filtering, pagination, issue details, localized
dates, and the English/Simplified Chinese UI can be evaluated without an admin
token.

For a link that opens the sample dashboard immediately, add `demo=1`:

```text
http://localhost:8080/?demo=1
http://localhost:8080/?demo=1&lang=zh-CN
```

The direct link works only while `ERROR_TRACER_DEMO_MODE=true`. Opening the demo
from the sign-in panel adds the same query parameter, so the current view can be
copied or bookmarked. Disconnecting removes it and returns to the private
sign-in panel.

## Security boundary

The demo is deliberately separate from production data:

- Sample events live in a dedicated in-memory store created at process start.
- Demo handlers never read from or write to the configured SQLite database.
- Only `GET` list and detail routes exist under `/api/v1/demo/issues`.
- The normal `/api/v1/issues` routes still require the admin bearer token.
- The dashboard disables status changes while the demo is active.
- `/api/v1/meta` reveals only whether demo mode is enabled.

Because the demo routes are public when enabled, do not put secrets or copied
production payloads into the built-in fixtures. Disable demo mode on deployments
that do not need a public product tour.

## Demo endpoints

| Method | Path | Result |
| --- | --- | --- |
| `GET` | `/api/v1/meta` | `{"demo_mode":true}` when enabled |
| `GET` | `/api/v1/demo/issues` | Paginated built-in issue list |
| `GET` | `/api/v1/demo/issues?status=open` | Built-in issues filtered by status |
| `GET` | `/api/v1/demo/issues/{fingerprint}` | One built-in issue and its latest event |

The list endpoint accepts the same bounded `limit`, `offset`, and `status`
parameters as the authenticated issue API. Attempts to `PATCH` a demo issue
return `405 Method Not Allowed`.

## Language selection

Use the dashboard selector to switch between English and Simplified Chinese, or
link directly to either locale:

```text
http://localhost:8080/?lang=en
http://localhost:8080/?lang=zh-CN
```

The language is initialized from the URL or compatible browser locale. The
selector updates the URL with `history.replaceState`; it does not use local
storage, session storage, or cookies. Event messages, stack traces, URLs, and
tags are user-controlled data and are intentionally not translated.
