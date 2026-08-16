# Maintainers

## Current Maintainers

| Area | Primary | Backup / escalation |
|---|---|---|
| Go API and data stores | `@ksuk` | Vacant |
| Native Go evaluation engine | `@ksuk` | Vacant |
| UI and documentation | `@ksuk` | Vacant |
| Security, releases, and migrations | `@ksuk` | Vacant |

This repository has one active maintainer, and both the rules below and the
mechanisms that enforce them are written for that. It operates under
[Single-Maintainer Operating Mode](docs/development/repository-governance.md),
decided in ADR-0016. Merging and releasing are both completable by one person;
what the resulting release does *not* assert is published with the release
itself. Naming a backup maintainer is how the project moves to separated
creation and approval, not a precondition it is currently blocked on.

## Review Rules

- All changes enter through a pull request; direct pushes to `main` are not a
  release process.
- No pull request is approved. With one maintainer there is nobody to approve
  it, so instead every pull request carries a self-review record — intent,
  blast radius, rollback, gates passed, and what was **not** verified — checked
  by `Governance Required` and bound to the head commit. It is a compensating
  control, and calling it anything else would be the actual governance failure.
- Rule version activation still requires a different Admin than the one who
  created the version (ADR-0014). That control is enforced in the product, not
  in this repository, and it is the operator's two Admins that satisfy it.
- Database migrations and audit-role changes get the same self-review record as
  everything else, with the migration replay and audit-privilege gates in CI
  doing the work a second reviewer would otherwise do. This is weaker than a
  second Admin and is recorded as such in ADR-0016's tailoring record.
- Commits must include a DCO sign-off. CI is the final check, not a substitute
  for review — and here CI is administered by the same person it checks, which
  is why the rulesets are committed to `.github/rulesets/` and compared against
  the live configuration weekly.

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

Once that pull request lands, apply
`scripts/configure-github-ruleset.sh --apply --require-approvals`, refresh the
baseline in `.github/rulesets/`, and update the governance disclosure the
release workflow emits. ADR-0016 §6 lists the steps.
