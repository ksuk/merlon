---
title: Data Egress
---

# Data Egress

What Merlon sends outside your network, and what triggers it. This page exists
because "self-hosted" is a claim, and a reviewer is entitled to see it
enumerated rather than asserted.

## Summary

**Merlon makes no outbound connection you did not configure.** There is no
telemetry, no usage analytics, no crash reporting, no licence check, and no
update check.

A default deployment — one where no adapter is configured, no screening list
source is set, and no webhook subscription exists — makes exactly one outbound
connection: to your own PostgreSQL database.

## The complete list

Every outbound connection the application can make. There are three, and each
exists only when you configure it.

| # | Destination | Triggered by | Carries |
|---|---|---|---|
| 1 | Your PostgreSQL server | Always | All application data |
| 2 | Screening list sources you configure | The scheduled list import job | Nothing outbound beyond the HTTP request; the response is the list |
| 3 | Webhook URLs you subscribe | Events matching an active subscription | The event payload |
| 4 | REST endpoints in your adapter configuration | Ingestion from your core banking or wallet system | Query parameters for the records being fetched |

There is no fifth. This is verifiable: the outbound HTTP call sites in the
codebase are `internal/screening/adapter.go`, `internal/server/webhook.go`, and
`internal/adapter/rest.go`.

### 1. PostgreSQL

Configured by `MERLON_DATABASE_URL` and `MERLON_MIGRATION_DATABASE_URL`.
Normally inside your own network. Merlon does not choose this destination.

### 2. Screening list imports

Sanctions and PEP list sources are configured by you. If your lists are hosted
internally, this connection never leaves your network.

On a failed fetch, Merlon keeps matching against the last successfully imported
list rather than failing open, and raises an operational alert after three
consecutive failures. It does not fall back to any alternative source.

### 3. Webhook deliveries

Only to URLs in subscriptions you created. Payloads are the event data you
subscribed to.

The delivery client resolves the destination and **refuses to connect to
private, loopback, or link-local addresses**, and re-validates on every
redirect. This is server-side request forgery protection: it stops a webhook
subscription from being used to reach services inside your network that are not
otherwise exposed.

### 4. Adapter ingestion

Configured through `MERLON_ADAPTER_CONFIG_PATH`. These are your systems, at
addresses you specify. The adapter uses a restricted transport governed by the
adapter security configuration.

## What is not there

| Not present | Notes |
|---|---|
| Product telemetry / usage analytics | No such code exists; nothing to disable |
| Crash or error reporting to a vendor | Errors go to your logs only |
| Licence key validation | There is no licence key mechanism |
| Update / version check | Not implemented; see below |
| Third-party fonts, scripts, or CDN assets in the UI | The UI bundle is self-contained and served by the Go binary |
| Analytics on the operator UI | None |

You do not need an environment variable to turn any of this off, because none
of it is there to turn off.

### On update checking

Merlon does not tell you when a new version exists. That is a real operational
cost: an operator can run a version with a published vulnerability without
being prompted.

It is deliberate. Merlon is deployed by institutions that frequently run it on
closed networks, where an unexplained outbound request to a public host is a
finding in its own right, and where "the software phones a vendor" is a question
that has to be answered for every deployment rather than once.

Track releases yourself: watch
[Releases](https://github.com/ksuk/merlon/releases) or subscribe to the release
feed, and see [Upgrading](../operations/upgrade.md) for what to do about a new
one. `GET /healthz` reports the version you are running.

## Verifying this yourself

Do not take this page's word for it. On a deployment with no adapter, no
screening source, and no webhook subscriptions, capture egress from the
container and confirm PostgreSQL is the only destination:

```bash
# Everything the container tries to reach, excluding your database host.
docker run --rm --network container:<merlon-container> nicolaka/netshoot \
  tcpdump -n 'tcp[tcpflags] & tcp-syn != 0 and tcp[tcpflags] & tcp-ack == 0'
```

Or deny egress outright and confirm Merlon still starts, serves, scores, and
monitors — it will, because nothing in that path leaves your network.

## Data residency

All customer data, transaction data, screening results, cases, STR drafts, and
audit records live in your PostgreSQL database, in your infrastructure, under
your jurisdiction. Merlon has no cloud component, no vendor-hosted service, and
no account to register.

Direct-PII customer attributes are encrypted at rest, with keys held outside
the database in `MERLON_ENCRYPTION_KEY_RING`. See
[Backup and Restore](../operations/backup-restore.md).
