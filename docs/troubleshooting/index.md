---
title: Troubleshooting
---

# Troubleshooting

Find the message you are actually seeing. Each row links to the section that
explains it.

## Symptom index

| What you are seeing | Go to |
|---|---|
| `set MERLON_POSTGRES_PASSWORD` or `set MERLON_BOOTSTRAP_TOKEN`, and nothing starts | [Compose stops before any container starts](#compose-stops-before-any-container-starts) |
| `docker compose up --wait` hangs, or a readiness probe fails, on a new deployment | [Readiness stays 503 until setup is complete](#readiness-stays-503-until-setup-is-complete) |
| A login screen, and no account to log in with | [There is no account to log in with](#there-is-no-account-to-log-in-with) |
| `initial setup has already been completed` | [`/setup` returns 409](#setup-returns-409) |
| `MERLON_AUTH_ENABLED must be true in production` | [Production refuses to start](#production-refuses-to-start) |
| `MERLON_DATABASE_URL must be set in production` | [Production refuses to start](#production-refuses-to-start) |
| `MERLON_ENCRYPTION_KEY_RING must be set in production` | [Production refuses to start](#production-refuses-to-start) |
| `MERLON_SEED must not be true in production` | [Production refuses to start](#production-refuses-to-start) |
| `MERLON_TRUSTED_PROXY_CIDRS must be set when MERLON_RATE_LIMIT is enabled in production` | [Production refuses to start](#production-refuses-to-start) |
| `database audit privilege preflight failed` | [Audit privileges are not hardened](#audit-privileges-are-not-hardened) |
| `native Go engine unavailable`, `read CDD config` | [Scores and alerts are never produced](#scores-and-alerts-are-never-produced) |
| `MERLON_ENCRYPTION_KEY_RING not set, customer PII fields will be stored in plaintext` | [PII was written unencrypted](#pii-was-written-unencrypted) |
| `MERLON_MIGRATION_DATABASE_URL is required in production` | [Migrations](database.md#the-migration-role-is-missing) |
| `using MERLON_DATABASE_URL as migration role` | [Migrations](database.md#the-migration-role-is-missing) |
| `checksum mismatch: ledger=… file=…` | [Migrations](database.md#a-migration-checksum-does-not-match) |
| `does not match a migration filename` | [Migrations](database.md#an-existing-database-has-no-ledger) |
| Customer records readable but unreadable after a restore | [Migrations](database.md#restored-data-cannot-be-decrypted) |
| Webhook subscribers stopped receiving events | [Webhook deliveries stop](#webhook-deliveries-stop) |
| Sanctions or PEP screening results look stale | [Screening results are stale](#screening-results-are-stale) |

## Before you start

Two things resolve a large share of reports on their own.

**Check what version you are actually running.** `GET /healthz` returns it. If
it reports `dev`, the image was built without a `VERSION` build argument and is
not a released build — see [Container Images](../operations/container-images.md).

```bash
curl -s http://localhost:8080/healthz
```

**Check readiness rather than liveness.** `GET /healthz` says the process is
up. `GET /healthz/ready` says whether it can actually serve, and names the
failing subsystem:

```bash
curl -s http://localhost:8080/healthz/ready
# {"checks":{"setup":"ok","postgres":"ok","engine":"ok"},"status":"healthy"}
```

Every check that fails appears in `checks` with its error. Read that before
reading logs.

## Startup and first run

### Compose stops before any container starts

```
error while interpolating services.db.environment.POSTGRES_PASSWORD:
required variable MERLON_POSTGRES_PASSWORD is missing a value: set MERLON_POSTGRES_PASSWORD
```

The compose files deliberately have no default for the database password or the
bootstrap token, so a deployment cannot inherit a well-known credential by
accident. Compose reads them from `.env` in the repository root, which is not
committed:

```bash
cp .env.example .env
docker compose up --build
```

The values in `.env.example` are development-only and marked as such. Replace
them before this runs anywhere but your own machine.

### Readiness stays 503 until setup is complete

This is expected on a new deployment and is not a fault.

The image healthcheck probes `GET /healthz/live`, so `docker ps` shows the
container as `healthy` as soon as the process responds — before setup, and
without a database.

Readiness is a different question. `GET /healthz/ready` includes "an
administrator account exists", so until you complete
[initial setup](#there-is-no-account-to-log-in-with) it returns `503` with:

```json
{"checks":{"setup":"error: initial setup not completed"},"status":"unhealthy"}
```

That matters wherever readiness is deliberately gated on:

- a Kubernetes `readinessProbe` on `/healthz/ready`, which keeps the pod out of
  the Service endpoints;
- `docker compose up --wait`, which does not return;
- `depends_on: condition: service_healthy` against a compose healthcheck that
  probes readiness — the demo compose file does this on purpose, so that
  "healthy" means the demo is actually usable.

In those environments, complete setup during the initial rollout. Readiness
turns `healthy` within one probe interval of the first administrator being
created.

Images built before the healthcheck moved to liveness probed readiness at the
image level. With an older image the container itself stays `unhealthy` until
setup, and an orchestrator that restarts unhealthy containers will kill it
first: complete setup during the rollout, or raise the healthcheck start
period.

### There is no account to log in with

A new deployment has no users, so there is nothing to log in with and no
authenticated path to create one. Use initial setup:

- In the browser: follow **Create the administrator account** on the login
  screen, or open `/setup` directly.
- Over the API: `POST /api/v1/setup` with `{"email": "...", "password": "..."}`.

The password must be at least 12 characters. The account created is an Admin.
The current release has no supported flow for creating later accounts:
**User management** and `GET /api/v1/admin/users` only list existing users.

### `/setup` returns 409

```
initial setup has already been completed
```

Initial setup succeeds exactly once, by design — otherwise it would be a
standing route for minting administrators on a live system. An account already
exists.

If nobody knows its credentials, this is a password reset against the database,
not a setup problem. There is no supported flow for creating a second first
administrator.

### Production refuses to start

`MERLON_ENV=production` enables configuration checks that fail closed. Each
message names the variable to set:

| Message | Meaning |
|---|---|
| `MERLON_AUTH_ENABLED must be true in production` | Authentication cannot be disabled outside development and demo |
| `MERLON_DATABASE_URL must be set in production` | The in-memory store is not a production store; it loses everything on restart |
| `MERLON_ENCRYPTION_KEY_RING must be set in production` | Direct-PII customer attributes must be encrypted at rest |
| `MERLON_SEED must not be true in production` | Seeding would write synthetic customers into a real system |
| `MERLON_TRUSTED_PROXY_CIDRS must be set when MERLON_RATE_LIMIT is enabled in production` | Rate limiting behind a proxy needs to know which forwarding headers to trust, or any client can spoof its address and evade the limit |

These are refusals to start, not warnings. Setting `MERLON_ENV` to something
other than `production` to get past them removes the checks without removing
the exposure. See [Configuration](../configuration.md).

### Audit privileges are not hardened

```
database audit privilege preflight failed
```

In production the API verifies that the serving role cannot modify
`audit_logs` before it will start. An append-only audit trail the application
can rewrite is not an audit trail.

Apply the grants with the migration role:

```bash
MERLON_MIGRATION_DATABASE_URL=... make audit-harden
```

See [Backup and Restore](../operations/backup-restore.md) and
`docs/operations/audit-hardening.sql`.

### Scores and alerts are never produced

```
{"level":"WARN","msg":"native Go engine unavailable","error":"read CDD config: open cdd_weights.yaml: no such file or directory"}
```

The scoring and transaction-monitoring engine could not load its rule
configuration, so the API starts and serves but never scores anything. Because
this is a warning rather than a startup failure, it is easy to miss and then
read as "no alerts were generated", which is not the same as "no alerts were
warranted".

Point the configuration variables at real files — the supplied compose files
mount `./content` for this — and confirm with:

```bash
curl -s http://localhost:8080/healthz/ready
```

`checks.engine` must be `ok`. See [Rule Authoring](../rule-authoring.md).

### PII was written unencrypted

```
{"level":"WARN","msg":"MERLON_ENCRYPTION_KEY_RING not set, customer PII fields will be stored in plaintext"}
```

Outside production this is a warning, not an error, so a long-running
evaluation instance can accumulate plaintext customer attributes.

Setting the key ring later encrypts new writes; it does not retroactively
encrypt rows already written. If real data was loaded while this warning was
being emitted, treat those rows as plaintext-at-rest and re-write or reload
them. See [Data Retention](../compliance/data-retention.md).

## Integrations

### Webhook deliveries stop

A delivery is retried up to 10 attempts. After that the event moves to the dead
letter queue rather than being dropped, so the events are still there.

Inspect and reprocess the DLQ through the webhook administration API and the
**Webhooks** page. A subscriber that failed continuously has a DLQ backlog to
replay once it is healthy — reprocessing is an explicit action, not automatic,
so replaying into a still-broken endpoint is not possible by accident.

### Screening results are stale

On a failed list fetch, Merlon keeps matching against the last list that
imported successfully instead of failing open with an empty list. Screening
therefore keeps working, and the failure is not visible as missing results.

Consecutive failures are counted per list, and after three in a row Merlon flags
the failure for operators: the import job logs a structured error with
`needs_operational_alert=true`, the dashboard's screening-freshness data carries
the same flag, and the `merlon_screening_list_stale_days` gauge reports each
list's age on `/metrics`. Merlon does not notify anyone itself. If nothing in
your deployment routes those to a human, the staleness goes unnoticed — that is
a property of your monitoring, not something Merlon does on its own.

If screening results look older than expected, check the import job for that
list rather than the matching logic. The results are stale, not wrong.

## Still stuck

Open an issue with the version from `GET /healthz`, the full
`GET /healthz/ready` body, and the container logs from startup. Remove
credentials and customer data first.
