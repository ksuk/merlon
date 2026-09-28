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

## Realtime monitoring recovery

`POST /api/v1/transactions` does not report a monitoring pass when the
monitoring dependency is not configured. It atomically stores the transaction,
its mutation audit, one pending evaluation, and the queue audit, then returns
the transaction with a `monitoring_evaluation` object. The object identifies
the durable queue row and its current status. An idempotent replay returns the
same transaction and queue row instead of creating duplicates.

The recovery loop in `worker` or `all` mode claims the pending row after the
monitoring dependency becomes available. Transaction detail responses continue
to expose the current queue status, including `RESOLVED`, so an accepted
transaction never appears to have been evaluated merely because it was stored.
If the API cannot persist the pending evaluation and audit in the same database
transaction, transaction creation fails and no transaction is committed.

Backtest requests are durable rows. The API rejects a new job before
persistence when its native execution engine is unavailable. An API-only
deployment with a loaded engine can persist jobs for a separate worker; a
worker (or `all`) deployment claims queued rows with a database lease and
reclaims an expired running lease after restart.

An accepted job cannot remain queued indefinitely. Every API and worker
process runs a database-backed lifecycle monitor. If no worker claims a row
within `MERLON_BACKTEST_QUEUE_TIMEOUT` (default `10m`), the row becomes
`failed` with a stable retryable reason. `POST /api/v1/backtests/{id}/retry`
returns the same job identifier to `queued`; repeated requests while it is
already queued or running are idempotent. Retry clears partial result rows in
the same transaction before another worker can claim the job, so a rerun does
not accumulate duplicate results.

Worker failures store a stable public error rather than an internal engine or
dependency error. Diagnose the underlying cause from protected process logs,
restore the dependency, and retry the failed job. Audit entries record job
start, failure, completion, retry requests, and queue expiry. The
`merlon_backtest_job_transitions_total{transition=...}` counter exposes the
same lifecycle categories without job identifiers.

Jobs snapshot `[from,to)` and config digests at creation, report progress/ETA,
and never create alerts or cases. Non-`active` baseline/candidate rule
references are resolved and their versioned definitions are pinned on job
creation, so a queued job cannot drift when an operator publishes a new rule
version; an unresolved reference fails closed.
