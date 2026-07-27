---
title: API / Worker Mode
---

# API / Worker Mode

PH9 uses one `merlon-api` image with an explicit ownership mode:

| `MERLON_MODE` | Owns |
|---|---|
| `all` (default) | HTTP, realtime monitoring, screening imports/rescreening, notifications, retention, recovery, TM batch, and durable backtests |
| `api` | HTTP/realtime paths plus screening imports/rescreening, notifications, retention, and EDD |
| `worker` | Recovery, scheduled TM batch, and durable backtests; health/control HTTP listens on `MERLON_WORKER_HTTP_ADDR` |

The split is optional. A two-container deployment shares only PostgreSQL and
uses no queue or cache:

```yaml
services:
  api:
    image: merlon-api:latest
    environment:
      MERLON_MODE: api
      MERLON_DATABASE_URL: postgres://merlon:secret@postgres/merlon
  worker:
    image: merlon-api:latest
    environment:
      MERLON_MODE: worker
      MERLON_WORKER_HTTP_ADDR: :8081
      MERLON_DATABASE_URL: postgres://merlon:secret@postgres/merlon
```

The image healthcheck follows the selected process mode. In `worker` mode it
probes liveness on `MERLON_WORKER_HTTP_ADDR` (default `:8081`), not the API
listener.

Backtest requests are durable rows. An API-only deployment accepts and
persists jobs; a worker (or `all`) deployment claims queued rows with a
database lease and resumes them after restart. Jobs snapshot `[from,to)` and
config digests at creation, report progress/ETA, and never create alerts or
cases. Non-`active` baseline/candidate rule references are resolved and their
versioned definitions are pinned on job creation, so a queued job cannot drift
when an operator publishes a new rule version; an unresolved reference fails
closed.
