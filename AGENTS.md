# Merlon

Instructions for coding agents working in this repository. This is the single
source of truth; `CLAUDE.md` imports this file.

## Overview

Merlon — self-hosted AML/CFT software for non-bank financial institutions in Japan. Monorepo with Go API + React UI.

## Commands

```bash
make build    # Build the Go API and UI
make test     # Run all tests (Go + UI)
make lint     # Run all linters
make fmt      # Format all code
make seed     # Load demo data
```

Per-component commands are in each subdirectory's AGENTS.md.

## Architecture

```
React UI (Vite) → REST → Go API (native engine)
                            ↓
                        PostgreSQL
```

| Component | Directory | Role |
|---|---|---|
| Go API | `api/` | CRUD and business orchestration (net/http, pgx) |
| React UI | `ui/` | Vite + React 19 + Tailwind CSS v4 + React Router v8 |
| Native engine | `api/internal/engine/native/` | CDD scoring, TM evaluation, screening, backtesting |
| Rule configs | `content/` | CDD weights and TM scenarios (JSON/YAML) |

When `MERLON_DATABASE_URL` is unset, the API runs with an in-memory store. Set `MERLON_SEED=true` to load demo data.

## Design Principles

These are system-wide decision criteria. Refer to them during implementation and review.

- **Score-Driven Architecture** — CDD score is the central axis. TM thresholds, case priority, and screening frequency are all derived from the CDD score (ADR-0004)
- **Auditability First** — Record all decision rationale reproducibly. Pin deterministic output for identical input via tests
- **Fail-Alert** — On failure, err toward alerting (prefer false positives over missed detections)
- **Configuration as the Product** — Express rules as JSON/YAML config. Never hardcode
- **Contract Stability** — REST/OpenAPI contracts must maintain backward compatibility for 12+ months

## Testing

TDD workflow: write test → implement → refactor. Follow existing frameworks and naming conventions in each language.

## Conventions

- Commit with `git commit -s`. The DCO sign-off is required; the `check-signoffs` gate rejects commits without it.
- Use Conventional Commits: `<type>(<scope>): <description>`. Do not add AI-generated trailers such as `Co-Authored-By` or `Generated with`.
- Never push directly to `main` and never force-push a shared branch. Branch prefixes: `feat/ fix/ docs/ refactor/ test/ chore/ ci/ perf/`.
- Every PR states the requirement or issue, the public ADR or design document, the test evidence, and the rollback plan. The Traceability workflow enforces this.
- Required local gates before opening a PR: `make lint`, `make test`, `make docs-check`.
- Details: [CONTRIBUTING.md](CONTRIBUTING.md) and [repository governance](docs/development/repository-governance.md).

## Gotchas

- Killing a Docusaurus build in `website/` with `kill -9` leaves a poisoned cache that hangs every later build. Remove `website/node_modules/.cache` to recover.
- `.gitattributes` pins YAML, shell scripts, and Dockerfiles to LF. Do not write them back with CRLF — `docker-compose.yml` has drifted into mixed line endings before.
- Generated files are not committed: `docs/api/`, `docs/release-notes.md`, and `deploy/seed/demo/*.json`. Regenerate them (`make generate-openapi`, `make demogen`) rather than editing them.
