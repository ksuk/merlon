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

## Option 2: Local environment

If you're not using the DevContainer, install the following tools yourself.

### Go 1.25+

```bash
# Get it from https://go.dev/dl/, or via your package manager
go version   # confirm go1.25 or higher
```

### Node.js 20+ / npm

```bash
# Get the LTS release from https://nodejs.org/, or use nvm
node --version   # v20 or higher
npm --version
```

### PostgreSQL 16+

Running it via Docker is recommended.

```bash
docker run -d --name merlon-db \
  -e POSTGRES_USER=merlon \
  -e POSTGRES_PASSWORD=merlon \
  -e POSTGRES_DB=merlon \
  -p 5432:5432 \
  postgres:16
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
| `make minimal-up` / `make minimal-down` | Start/stop the minimal topology (PostgreSQL + API only) |
| `make generate-openapi` | Export the OpenAPI spec to `docs/api/openapi.json` |

To seed demo data into an already-running PostgreSQL instance instead of
starting the whole compose topology, run `scripts/seed-demo.sh`, which loads
`deploy/seed/seed.sql` via `psql`.

See [testing.md](testing.md) for more detail.
