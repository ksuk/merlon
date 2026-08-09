---
title: Development Environment Setup
---

# Development Environment Setup

How to set up a Merlon development environment. Two paths are covered: a
DevContainer (recommended) and a local install.

## Option 1: DevContainer (recommended)

With [VS Code](https://code.visualstudio.com/), the
[Dev Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers),
and Docker, the whole toolchain is set up for you automatically.

1. Open the repository in VS Code.
2. Open the command palette (`F1`) and run **Dev Containers: Reopen in
   Container**.
3. The first build takes a few minutes while the image is built. Once it's
done, Go and Node.js are available, along with `psql`
   (the PostgreSQL client), `gh` (GitHub CLI), `claude` (Claude Code CLI),
   `codex` (OpenAI Codex CLI), and `wrangler` (Cloudflare CLI).

See `.devcontainer/devcontainer.json` and `.devcontainer/Dockerfile` for the
configuration.

## Compose project and port isolation

Give each local stack an explicit Compose project name with `-p`. Compose then
prefixes named volumes and networks with that project name, so separate stacks
do not share their database resources. The standard topology keeps API port
`8080` by default and does not publish PostgreSQL to the host:

```bash
docker compose -p merlon-dev -f docker-compose.yml up --build
```

When another stack already uses the API port, override only the host-side port
and use a separate project name:

```bash
MERLON_API_HOST_PORT=18050 \
  docker compose -p merlon-dev-18050 -f docker-compose.yml up --build
```

For local tests that need a host PostgreSQL client, add the test-only overlay
and choose a loopback database port. Do not add this overlay to a normal or
public deployment:

```bash
MERLON_API_HOST_PORT=18050 MERLON_DB_HOST_PORT=15450 \
  docker compose -p merlon-test-18050 \
  -f docker-compose.yml -f docker-compose.test.yml up --build
```

Use the same `-p` value and compose files for `down`. The demo topology uses
the same API override and keeps its API binding on `127.0.0.1`:

```bash
MERLON_API_HOST_PORT=18050 \
  docker compose -p merlon-demo-18050 -f docker-compose.demo.yml up --build
```

## Option 2: Local environment

If you're not using the DevContainer, install the following tools yourself.

### Go 1.26+

```bash
# Get it from https://go.dev/dl/, or via your package manager
go version   # confirm go1.26 or higher
```

### Node.js 26+ / npm

```bash
# Get the LTS release from https://nodejs.org/, or use nvm
node --version   # v26 or higher
npm --version
```

### PostgreSQL 18+

Running it via Docker is recommended.

```bash
docker run -d --name merlon-db \
  -e POSTGRES_USER=merlon \
  -e POSTGRES_PASSWORD=merlon \
  -e POSTGRES_DB=merlon \
  -p 5432:5432 \
  postgres:18
```

## First-time setup

```bash
# 1. Environment variables
cp .env.example .env

# 2. Fetch dependencies
cd api && go mod download && cd ..
cd ui && npm install && cd ..

# 3. Run DB migrations
make migrate

# 5. Load demo data (optional)
make seed
```

## Make targets

| Target | Description |
|---|---|
| `make build` | Build the Go API and UI |
| `make test` | Run Go and UI tests |
| `make lint` | Run all linters |
| `make fmt` | Format all code (Go, UI) |
| `make migrate` | Apply DB migrations with a checksum ledger using `MERLON_MIGRATION_DATABASE_URL` |
| `make seed` | Start the full docker-compose topology with demo data (`MERLON_SEED=true docker compose up --build`) |
| `make dev-up` / `make dev-down` | Start/stop the development topology (`docker-compose.yml` + `docker-compose.dev.yml`) |
| `make up` / `make down` | Start/stop the standard topology (PostgreSQL + API) |
| `make generate-openapi` | Export the OpenAPI spec to `docs/api/openapi.json` |

To seed demo data into an already-running PostgreSQL instance instead of
starting the whole compose topology, run `scripts/seed-demo.sh`, which loads
`deploy/seed/legacy/seed.sql` via `psql`. That file predates the current
schema (see `deploy/seed/legacy/README.md`); prefer `MERLON_SEED=true` with
`make seed` for a working demo dataset.

See [testing.md](testing.md) for more detail.
