---
title: Repository Governance
---

# Repository Governance

This page is the public record for ownership, review, release, and audit
dispositions. It deliberately does not copy private planning material.

## Change Control

- Changes are proposed through GitHub pull requests from a non-`main` branch.
- Required local gates are `make lint`, `make test`, and `make docs-check`.
- CI runs the same component gates and records coverage and SBOM artifacts.
- The author cannot approve their own change. Rule activation follows the
  two-Admin rule documented in the rules API.
- Production database migrations use `MERLON_MIGRATION_DATABASE_URL`; the API
  role is denied UPDATE and DELETE on `audit_logs` and is rejected at startup
  if it owns that table.

## Repository Limits

The repository is private on GitHub Free. Required branch-protection reviewers
cannot currently be configured for private repositories on that plan. Until
the repository is public or moved to a plan that supports those rules, review
is enforced by CODEOWNERS, the pull-request template, CI, and the two-Admin
operational procedure. Re-check this limitation before the first production
release.

## Accepted Historical Dispositions

- Existing history is not rewritten and no force-push policy applies.
- The large historical implementation commit is retained for provenance.
- No release tag is fabricated before the first release criteria are met.
- A second maintainer and signed release/provenance artifacts remain explicit
  pre-release work; see `MAINTAINERS.md` and the release checklist.
