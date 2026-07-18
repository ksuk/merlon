---
sidebar_position: 1
title: Getting Started
---

# Getting Started

A quick-start guide to running Merlon with the least effort.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) 24+
- [Docker Compose](https://docs.docker.com/compose/) v2+
- `git`

You do not need Go or Node.js installed locally — the minimal setup
runs entirely in containers.

## Steps

```bash
# 1. Clone the repository
git clone https://github.com/ksuk/merlon.git
cd merlon

# 2. Prepare the environment file
cp .env.example .env

# 3. Start the minimal topology (API + PostgreSQL)
docker compose -f docker-compose.minimal.yml up --build

# 4. Health check (in another terminal)
curl localhost:8080/healthz
```

A response body containing `"status":"ok"` means the API started successfully.

## Next steps

- [Architecture overview](architecture.md) — understand the overall system layout
- [Configuration reference](configuration.md) — tune environment variables and `config.yaml`
- [Development environment setup](development/setup.md) — for developers who will edit code
- [Testing guide](development/testing.md) — how to run the test suites
