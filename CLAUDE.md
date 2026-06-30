# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Merlon — self-hosted AML/CFT software for non-bank financial institutions in Japan. Monorepo with Go API + Rust Engine + React UI.

## Commands

```bash
make build    # Build all components
make test     # Run all tests (Go + Rust + UI)
make lint     # Run all linters
make fmt      # Format all code
make proto    # Generate Go/Rust code from proto (buf lint + buf generate)
make seed     # Load demo data
```

Per-component commands are in each subdirectory's CLAUDE.md.

## Architecture

```
React UI (Vite) → REST → Go API → gRPC (protobuf) → Rust Engine
                            ↓
                        PostgreSQL
```

| Component | Directory | Role |
|---|---|---|
| Go API | `api/` | CRUD and business orchestration (net/http, pgx) |
| Rust Engine | `engine/` | CDD scoring, TM evaluation, screening, backtesting |
| React UI | `ui/` | Vite + React 19 + Tailwind CSS v4 + React Router v7 |
| Proto definitions | `proto/merlon/v1/` | gRPC contract between Go and Rust (managed by buf) |
| Rule configs | `content/` | CDD weights and TM scenarios (JSON/YAML) |

When `MERLON_DATABASE_URL` is unset, the API runs with an in-memory store. Set `MERLON_SEED=true` to load demo data.

## Design Principles

These are system-wide decision criteria. Refer to them during implementation and review.

- **Score-Driven Architecture** — CDD score is the central axis. TM thresholds, case priority, and screening frequency are all derived from the CDD score (ADR-0004)
- **Auditability First** — Record all decision rationale reproducibly. Pin deterministic output for identical input via tests
- **Fail-Alert** — On failure, err toward alerting (prefer false positives over missed detections)
- **Configuration as the Product** — Express rules as JSON/YAML config. Never hardcode
- **Contract Stability** — Proto-defined external contracts must maintain backward compatibility for 12+ months

## Testing

TDD workflow: write test → implement → refactor. Follow existing frameworks and naming conventions in each language.
