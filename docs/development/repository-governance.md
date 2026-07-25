---
title: Repository Governance
---

# Repository Governance

This page is the public record for ownership, review, release, and audit
dispositions. It deliberately does not copy private planning material.

## Change Control

- Changes are proposed through GitHub pull requests from a non-`main` branch.
- Required local gates are `make lint`, `make test`, and `make docs-check`.
  Go CI additionally calls the same `make verify-go` entry point, and the
  PostgreSQL job applies every migration twice before integration tests.
- Every PR and human-authored commit links a public issue as described in
  [Change Traceability](./change-traceability.md). The Traceability workflow
  rejects missing requirement, design, or commit references.
- The author cannot approve their own change. Database migrations and audit
  role changes require an independent Admin review. This is currently a
  convention: see [Protected Main Configuration](#protected-main-configuration)
  for what the `main` ruleset does and does not block on today.
- Rules API versions are inactive when created. A different authenticated
  Admin changes their active state, with the exact version decision and
  approval event committed atomically under ADR-0014.
- Production migrations use `MERLON_MIGRATION_DATABASE_URL`. The API role is
  denied `UPDATE` and `DELETE` on `audit_logs` and
  `rule_activation_events`, and production startup rejects unsafe table
  ownership or privileges.

## Protected Main Configuration

The `main` ruleset is applied in two phases. Phase 1 is everything a
single-maintainer repository can enforce server-side. Phase 2 adds the review
requirements and cannot be switched on until a second maintainer exists.

### Enforced now (phase 1)

- Pull requests only, squash merge only, linear history, and prevention of
  branch deletion and force pushes.
- Stale-review dismissal on push and resolved review threads.
- These status checks on the latest commit: `CI Required`,
  `Security Required`, `Traceability Required`, and `check-signoffs`. GitHub
  matches a required check by its check-run name, which is the job name, not a
  `<workflow> / <job>` path. A context that never reports leaves every pull
  request pending, so only checks that run unconditionally on pull requests
  targeting `main` are listed.

### Not yet enforced

Recorded 2026-07-25. These are declared controls that GitHub does not currently
block on. They are listed here rather than dropped, because a control this page
claims but does not implement is worse than a gap this page names.

- **Independent approval, CODEOWNERS review, and last-push approval.** GitHub
  does not let an author approve their own pull request, and
  `.github/CODEOWNERS` names a single owner. Enforcing them today would make
  every pull request unmergeable, including the one that adds a second
  maintainer. Until then, "the author cannot approve their own change" under
  [Change Control](#change-control) is a convention rather than a server-side
  block, and production release stays blocked — the
  [Release Checklist](./release-checklist.md) already makes a backup maintainer
  a precondition. Lift by adding the second maintainer to `.github/CODEOWNERS`
  and re-running the ruleset script with `--require-approvals`.
- **Documentation site build.** `Build & Check Docs Site` is path-filtered, so
  it does not report at all on pull requests that touch no documentation path.
  A required check that never reports blocks merges permanently, so it is not
  required. A failing docs build is visible on the pull request but does not
  block the merge.

The release-tag ruleset covers `v*.*.*` tags and prevents update and deletion.
The release workflow independently rejects lightweight tags, non-SemVer names,
and commits that are not reachable from `main`.

`scripts/configure-github-ruleset.sh` renders this configuration without
changing GitHub. An independent maintainer must review the output before an
authorized operator runs it with `--apply`, then export the active Ruleset API
response as release evidence. Renaming or removing a gating job changes its
check-run name, so the script must be re-applied in the same change. If the
repository plan does not provide these rules, production release remains
blocked; templates and CODEOWNERS are not a substitute for a server-side merge
block.

## Release Control

Only an annotated semantic-version tag reachable from `main` starts the
release workflow. It builds the pinned container image, publishes an immutable
digest, generates an SBOM and release manifest, and requests GitHub artifact
provenance attestation. Release approval, tag protection, restore evidence,
vulnerability-response exercise, backup maintainer, and three successful runs
of each required workflow are preconditions in the
[Release Checklist](./release-checklist.md).

## Accepted Historical Dispositions

- History reachable from `main` is not rewritten and no force-push remediation
  is used against it. An open pull request branch may still be rebased to
  correct commit metadata before merge.
- The large historical implementation commit is retained for provenance.
- No release tag is fabricated before the first release criteria are met.
- Historical changes without issue references remain historical evidence;
  the traceability gate applies to new pull requests and commits.
