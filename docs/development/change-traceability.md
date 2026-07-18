---
title: Change Traceability
---

# Change Traceability

Every implementation change must identify its public requirement, design
decision, test, and operational evidence. Private working notes are not
treated as repository evidence.

For every pull request:

- `Requirement / issue` contains `#<number>` or a public GitHub issue URL.
- `Public ADR or design reference` identifies a repository document. A
  justified `N/A — <reason>` is permitted when no design decision is involved.
- Every human-authored commit contains a `Refs #<number>` footer. Bot commits
  are excluded from this requirement.
- The PR records verification evidence and rollback or migration impact.

`scripts/check-traceability.sh` enforces these fields and footers in the
`Traceability / Required` pull-request check. The protected `main` ruleset must
also require CI, Security, DCO, independent approval, CODEOWNERS review, and
resolved review threads. `scripts/configure-github-ruleset.sh` renders the
reviewable ruleset payload and changes GitHub only when explicitly invoked
with `--apply`.

| Change area | Public requirement / decision | Implementation | Verification |
|---|---|---|---|
| CDD scoring and tier propagation | `docs/architecture.md`, CDD docs | `api/internal/engine/native` | Go tests and CI |
| Transaction monitoring and recovery | TM docs and `docs/operations/` | `api/internal/batch`, `api/internal/engine/native` | batch/recovery tests and metrics |
| Versioned rule activation | `docs/development/repository-governance.md` | `api/internal/server/rules.go`, `api/internal/store` | rule API/store tests |
| Audit retention and integrity | `docs/compliance/data-retention.md`, `docs/operations/audit-hardening.sql` | audit store, migration runner, startup preflight | migration integration checks and `merlon-audit verify` |
| API↔engine observability | `docs/architecture.md` and metrics endpoint | request-ID middleware and in-process metrics | API/engine tests and `/metrics` |
| Documentation and release gates | `CONTRIBUTING.md`, repository governance | `.github/workflows`, website checks | CI, `make docs-check`, SBOM and release provenance artifacts |

Pull requests must link the relevant public row or add a new row when a new
requirement is introduced. Release manifests preserve the tag, commit, image
digest, SBOM, and provenance relationship so the chain can be reconstructed
after merge.
