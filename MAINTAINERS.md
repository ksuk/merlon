# Maintainers

## Current Maintainers

| Area | Primary | Backup / escalation |
|---|---|---|
| Go API and data stores | `@ksuk` | Assign a second reviewer before the first production release |
| Native Go evaluation engine | `@ksuk` | Assign a second reviewer before the first production release |
| UI and documentation | `@ksuk` | Assign a second reviewer before the first production release |
| Security, releases, and migrations | `@ksuk` | Assign a second reviewer before the first production release |

This repository currently has one active maintainer. A production release is
blocked until a backup maintainer is named and has completed the onboarding
checklist below.

## Review Rules

- All changes enter through a pull request; direct pushes to `main` are not a
  release process.
- The author does not approve their own pull request or their own production
  deployment.
- Database migrations and audit-role changes require a second Admin reviewer.
- Commits must include a DCO sign-off. CI is the final check, not a substitute
  for review.

## Backup Onboarding

1. Read `CONTRIBUTING.md`, the security model, repository governance, the rule
   activation ADR, and the migration and backup/restore runbooks.
2. Run `make lint`, `make test`, `make docs-check`, and the PostgreSQL
   integration gate locally.
3. Review one API, engine, and migration pull request with the current owner,
   including an atomic rule-approval and audit-privilege review.
4. Participate in a sanitized restore drill and vulnerability-response
   tabletop, and review the resulting evidence against the release checklist.
5. Verify one image provenance attestation and match its digest to a release
   manifest in a non-production exercise.
6. Be added to this file and `.github/CODEOWNERS` in a separate pull request
   approved by someone other than the new backup maintainer.
