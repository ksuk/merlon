---
title: Change Traceability
---

# Change Traceability

Every implementation change must identify its public requirement, design
decision, test, and operational evidence. Private working notes are not
treated as repository evidence.

| Change area | Public requirement / decision | Implementation | Verification |
|---|---|---|---|
| CDD scoring and tier propagation | `docs/architecture.md`, CDD docs | `api/internal/cdd`, `engine/crates/merlon-engine/src/scoring` | Go/Rust tests and CI |
| Transaction monitoring and recovery | TM docs and `docs/operations/` | `api/internal/batch`, engine monitoring services | batch/recovery tests and metrics |
| Versioned rule activation | `docs/development/repository-governance.md` | `api/internal/server/rules.go`, `api/internal/store` | rule API/store tests |
| Audit retention and integrity | `docs/compliance/data-retention.md`, `docs/operations/audit-hardening.sql` | audit store, migration runner, startup preflight | migration integration checks and `merlon-audit verify` |
| API↔engine observability | `docs/architecture.md` and metrics endpoint | request-ID middleware, gRPC metadata, Rust tracing | API/engine tests and `/metrics` |
| Documentation and release gates | `CONTRIBUTING.md`, repository governance | `.github/workflows`, website checks | CI, `make docs-check`, SBOM artifacts |

Pull requests must link the relevant public row or add a new row when a new
requirement is introduced.
