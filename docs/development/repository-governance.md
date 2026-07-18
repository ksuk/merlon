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
  role changes require an independent Admin review.
- Rules API versions are inactive when created. A different authenticated
  Admin changes their active state, with the exact version decision and
  approval event committed atomically under ADR-0014.
- Production migrations use `MERLON_MIGRATION_DATABASE_URL`. The API role is
  denied `UPDATE` and `DELETE` on `audit_logs` and
  `rule_activation_events`, and production startup rejects unsafe table
  ownership or privileges.

## Protected Main Configuration

The `main` ruleset must require all of the following before merge:

- one approving review, CODEOWNERS review, last-push approval by someone other
  than the pusher, stale-review dismissal, and resolved review threads;
- `CI / Required`, `Security / Required`, `DCO / check-signoffs`, and
  `Traceability / Required` status checks on the latest commit;
- pull requests, squash merge only, linear history, and prevention of branch
  deletion and force pushes.

The release-tag ruleset covers `v*.*.*` tags and prevents update and deletion.
The release workflow independently rejects lightweight tags, non-SemVer names,
and commits that are not reachable from `main`.

`scripts/configure-github-ruleset.sh` renders this configuration without
changing GitHub. An independent maintainer must review the output before an
authorized operator runs it with `--apply`, then export the active Ruleset API
response as release evidence. If the repository plan does not provide these
rules, production release remains blocked; templates and CODEOWNERS are not a
substitute for a server-side merge block.

## Release Control

Only an annotated semantic-version tag reachable from `main` starts the
release workflow. It builds the pinned container image, publishes an immutable
digest, generates an SBOM and release manifest, and requests GitHub artifact
provenance attestation. Release approval, tag protection, restore evidence,
vulnerability-response exercise, backup maintainer, and three successful runs
of each required workflow are preconditions in the
[Release Checklist](./release-checklist.md).

## Accepted Historical Dispositions

- Existing history is not rewritten and no force-push remediation is used.
- The large historical implementation commit is retained for provenance.
- No release tag is fabricated before the first release criteria are met.
- Historical changes without issue references remain historical evidence;
  the traceability gate applies to new pull requests and commits.
