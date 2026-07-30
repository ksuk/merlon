---
sidebar_position: 1
title: Getting Started
---

# Getting Started

Running Merlon with the least effort, from nothing to a working operator
dashboard.

If you want to look at a populated system rather than an empty one, start with
the [Demo Tour](demo-tour.md) instead: it ships about 1,015 synthetic customers
and 98 alerts, and needs no account.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) 24+
- [Docker Compose](https://docs.docker.com/compose/) v2+
- `git`

You do not need Go or Node.js installed locally — everything below runs in
containers.

## 1. Start it

```bash
git clone https://github.com/ksuk/merlon.git
cd merlon
cp .env.example .env
docker compose up --build
```

The first build takes a few minutes. Compose reads `.env` for the database
password, bootstrap token, and development JWT signing secret, so the copy in
step two is not optional — without it, startup stops before Merlon can serve
the login flow.

Wait for this line:

```
{"level":"INFO","msg":"merlon-api starting","env":"development","mode":"all","addr":":8080"}
```

## 2. Create the administrator account

Open **[http://localhost:8080](http://localhost:8080)**.

Authentication is enabled in this topology and no account exists yet, so the
login screen is a dead end until you create one. Follow **Create the
administrator account** below the login form, or go straight to
[http://localhost:8080/setup](http://localhost:8080/setup).

Enter an email address and a password of at least 12 characters. This route
only works while no account exists — once the first administrator is created it
rejects further requests. This release does not provide a supported API or UI
for creating additional accounts; **User management** is a read-only list of
accounts that already exist.

:::note `healthy` does not mean set up

The container healthcheck asks `GET /healthz/live`, so `docker ps` reports the
API container as `healthy` as soon as the process is serving — before this
step. Readiness is separate: `GET /healthz/ready` returns `503` until the first
administrator exists, because an instance nobody can log in to is not ready to
serve. See [Troubleshooting](troubleshooting/index.md).

:::

## 3. Log in

Log in with the account you just created. You should land on the dashboard,
with an empty customer list and no alerts. That is expected: nothing has been
loaded yet.

## What next

| Goal | Go to |
|---|---|
| See the product working with data | [Demo Tour](demo-tour.md) |
| Load your own customers and transactions | [Initial Migration](operations/initial-migration.md) |
| Understand what drives scores and alerts | [Architecture](architecture.md) |
| Tune the rules | [Rule Authoring](rule-authoring.md) |
| Change any setting | [Configuration](configuration.md) |
| Run this for real | [Deployment](operations/deployment.md) |
| Something did not work | [Troubleshooting](troubleshooting/index.md) |

## Before this leaves your machine

`.env.example` ships development-only credentials, and every one of them is
marked `MUST change in production`. The compose file used above also publishes
port 8080 on all interfaces.

This quick start is for evaluation on a local machine. Read
[Deployment](operations/deployment.md) and [Configuration](configuration.md)
before running Merlon anywhere else — a system that produces regulatory records
under default development credentials is not one you want to explain later.
