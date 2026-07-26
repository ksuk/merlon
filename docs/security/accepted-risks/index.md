---
title: Accepted Risks
---

# Accepted Risks

Behaviour that a security review will flag, that Merlon does anyway, with the
reasoning. These are decisions, not defects — but they are decisions you are
inheriting, so they are written down rather than discovered.

Each entry states what the risk is, why the alternative was worse, and what
compensating control exists. If your own assessment reaches a different
conclusion, the entry tells you what to change.

## Initial setup is unauthenticated

**Risk.** `POST /api/v1/setup` creates the first administrator account without
authentication.

**Why.** A new deployment has no credentials, so there is no identity that
could authorize creating the first one. Every alternative — a build-time
default password, a shared bootstrap secret in the image, an out-of-band token
file — puts a usable credential somewhere before the operator has chosen one.

**Compensating controls.** The route succeeds exactly once: it returns `409`
as soon as any user row exists, so it cannot be replayed to mint a second
administrator later. `GET /healthz/ready` reports unhealthy until setup is
complete, making the window visible rather than silent.

**What you must do.** Do not expose a fresh instance to an untrusted network
before completing setup. The exposure window is between first start and first
administrator, and it is yours to keep short.

## The alert engine fails toward alerting

**Risk.** Ambiguous or degraded conditions produce alerts rather than silence.
This raises the false-positive rate and the review workload.

**Why.** The opposite failure mode is a missed detection, which is
indistinguishable from correct operation until an examiner finds it. A false
positive costs analyst time; a false negative costs a filing that should have
happened.

**Compensating control.** Alert suppression, whitelisting with dual control,
and backtesting against candidate rule sets exist so the rate can be tuned
deliberately rather than by weakening detection.

## Screening continues against a stale list

**Risk.** When a sanctions or PEP list source cannot be reached, Merlon keeps
matching against the last list that imported successfully. Screening decisions
may therefore be made against data that is hours or days old.

**Why.** The alternatives are to fail open — match against nothing, which
silently passes every customer — or to fail closed and stop screening
entirely, which halts onboarding on a transient network error.

**Compensating controls.** Consecutive failures are counted per list, and after
three in a row Merlon flags the condition: the import job logs a structured
error with `needs_operational_alert=true`, the dashboard's screening-freshness
data carries the same flag, and the `merlon_screening_list_stale_days` gauge
reports each list's age on `/metrics`. Merlon does not notify anyone itself —
there is no built-in notifier. Import state remains queryable, so the age of the
list behind any decision is recoverable.

**What you must do.** Wire those flags to a human yourself: alert on the log
field, or on `merlon_screening_list_stale_days`, in your own monitoring stack.
The control only works if something routes it to someone who reads it.

## Migrations are forward-only

**Risk.** There are no down migrations. Rolling back a release that changed the
schema requires restoring from backup, which means data written after the
backup is lost.

**Why.** A down migration that reverses a data transformation is either lossy
or incorrect, and an incident is the worst time to discover which. Restore is
slower and its properties are knowable in advance.

**Compensating controls.** The migration runner records a checksum per
migration and refuses to proceed when a file has changed since it was applied.
Applying twice is a no-op. The release checklist requires a rehearsed restore,
not a documented one.

**What you must do.** Test your restore. An untested backup is not a rollback
plan.

## A lost encryption key ring is unrecoverable

**Risk.** Direct-PII customer attributes are encrypted at rest with keys held
in `MERLON_ENCRYPTION_KEY_RING`, outside the database. A database backup
without the corresponding keys is permanently unreadable.

**Why.** Keys stored alongside the data they protect do not protect it. The
consequence is that key custody becomes an operational responsibility.

**Compensating controls.** Key rotation re-encrypts in batches online, so
rotation does not require downtime and does not force a big-bang cutover.

**What you must do.** Back up the key ring separately from the database, retain
retired keys for at least as long as you retain backups written under them, and
include an encrypted-read check in your restore exercise.

## No plugin or extension sandbox

**Risk.** There is no way to run third-party code inside Merlon. Integrations
that a plugin system would enable must instead be built as external services
against the REST API.

**Why.** In-process third-party code would execute inside the transaction
boundary that produces regulatory records. Extension happens through
configuration — rules, YAML adapters, webhooks — which is auditable in a way
that arbitrary code is not.

**Compensating controls.** The REST API, issuable API keys, configurable REST
adapters, and outbound webhooks with a dead-letter queue.

## The demo topology has authentication disabled

**Risk.** `docker-compose.demo.yml` runs with `MERLON_AUTH_ENABLED=false` and
seeded synthetic data.

**Why.** Its purpose is evaluation without setup friction.

**Compensating controls.** It binds to `127.0.0.1` only, its dataset is
entirely synthetic, and `MERLON_SEED=true` is rejected outright when
`MERLON_ENV=production`. `MERLON_AUTH_ENABLED=false` is likewise refused in
production.

**What you must do.** Never point this compose file at real data and never run
it on a reachable host.
