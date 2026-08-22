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
- No pull request is approved, because with one maintainer there is nobody to
  approve it. What is required instead is a self-review record, enforced by the
  `Governance Required` check — see
  [Single-Maintainer Operating Mode](#single-maintainer-operating-mode).
  ADR-0016 records the decision and what it does not achieve.
- Rules API versions are inactive when created. A different authenticated
  Admin changes their active state, with the exact version decision and
  approval event committed atomically under ADR-0014.
- Production migrations use `MERLON_MIGRATION_DATABASE_URL`. The API role is
  denied `UPDATE` and `DELETE` on `audit_logs` and
  `rule_activation_events`, and production startup rejects unsafe table
  ownership or privileges.

## Protected Main Configuration

The `main` ruleset has two phases. Phase 1 is what a single-maintainer
repository can enforce server-side, and is what runs today. Phase 2 adds the
approving-review requirements and is switched on when a second Admin exists —
see [Review date, and the path off this mode](#review-date-and-the-path-off-this-mode).

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
- `Governance Required` is the fifth. It is not in the live ruleset yet:
  `scripts/configure-github-ruleset.sh` lists it, and it is applied only after
  the workflow that reports it is on `main` and has been observed reporting on
  a pull request. The committed baseline in `.github/rulesets/` shows the live
  configuration, so until that step lands it shows four. That is the ordering
  described below, not a control being declared without being enforced.

### Not enforced, and why

- **Approving review, CODEOWNERS review, and last-push approval.** GitHub does
  not let an author approve their own pull request, and `.github/CODEOWNERS`
  names a single owner. Requiring them with one maintainer would make every
  pull request unmergeable, including the one that adds a second maintainer.
  The self-review record enforced by `Governance Required` is what stands in
  their place; it is a compensating control and not equivalent to any of them.
- **Documentation site build.** `Build & Check Docs Site` is path-filtered, so
  it does not report at all on pull requests that touch no documentation path.
  A required check that never reports blocks merges permanently, so it is not
  required. A failing docs build is visible on the pull request but does not
  block the merge.

The release-tag ruleset covers `v*.*.*` tags and prevents update and deletion.
The release workflow independently rejects lightweight tags, non-SemVer names,
pre-release identifiers, and commits that are not reachable from `main`.

`scripts/configure-github-ruleset.sh` renders this configuration without
changing GitHub. The rendered output is reviewed before an authorized operator
runs it with `--apply`, and the active Ruleset API response is exported and
committed to `.github/rulesets/`. That review is a self-review, not an
independent one; the baseline diff is what makes the change auditable
afterwards. Renaming or removing a gating job changes its check-run name, so
the script must be re-applied in the same change. Templates and CODEOWNERS are
not a substitute for a server-side merge block.

Adding a required context is ordered, not simultaneous: a context that never
reports leaves every pull request pending forever. Merge the workflow, watch it
report on a pull request, then re-apply the ruleset and refresh the baseline.

## Single-Maintainer Operating Mode

This repository has one active maintainer. Independent approval is therefore
impossible by definition, not merely unimplemented. This section describes what
applies instead. ADR-0016 records the decision, the rejected alternatives, and
the tailoring against the internal quality standard.

Every clause below is either **enforced by a mechanism** or a **plain statement
of fact**. Rules that are declared but not enforced are deliberately absent:
they read as controls, behave as nothing, and cost the most at audit. If
something here looks like a rule, a workflow implements it.

### One channel, and the disclosure travels with the artifact

The project publishes `vX.Y.Z`. Pre-release identifiers are rejected by the
release workflow's SemVer gate, and `scripts/changelog.mjs` requires every tag
to have its own `CHANGELOG.md` section with no fallback.

What a release does and does not assert is emitted with the release itself
rather than left in documentation the reader may not reach:

| Output | Carries |
|---|---|
| `release-manifest.json` | `governance: { mode, independent_approval, separation_of_duties, adr }` at `schema_version: 2` |
| Container image labels | `io.github.ksuk.merlon.governance.*`, the same four facts |
| GitHub release notes | A disclosure header above the changelog section |

So `docker inspect` answers the vendor-assessment question without anyone
reading this page. A `vX.Y.Z` tag here asserts that `CI Required` and
`Security Required` passed on the release commit, verified by the release
workflow before publication; pull requests merged after ADR-0016 also carry a
self-review record, enforced at merge time rather than re-checked at release
time. It does not assert independent approval or separation of duties, and says
so on the artifact.

### Merges require a self-review record

No pull request is approved, because there is nobody to approve it. What
replaces approval is a record of what the author did and — the part that
matters — what they did not verify.

`.github/workflows/governance.yml` posts the `Governance Required` commit
status against the pull request head. `scripts/check-self-review.mjs` decides
it: a comment following `.github/SELF_REVIEW_TEMPLATE.md` must exist, it must
have been posted **by the pull request author**, its `Head SHA` must match the
current head, and all five sections must be present and non-empty. Pushing new
commits invalidates the record — the same semantics as
`dismiss_stale_reviews_on_push`, reproduced for a repository with no reviews to
dismiss.

The author binding is not a formality. This repository is public and a head SHA
is public, so a record checked for shape alone is one any passer-by could post,
and the only review control the project has would be satisfiable by a stranger.
Comments from anyone other than the author are not records.

Deleting the record re-opens the gate. The workflow subscribes to
`issue_comment` deletions as well as creations and edits, because the verdict
is a commit status that persists once posted: without the deletion event, a
green `Governance Required` would outlive the evidence that produced it.

The status is a commit status rather than a check-run because the record
arrives as a comment, after the push that produced the head commit. A ruleset
accepts either.

The trigger is `pull_request_target`, not `pull_request`. It runs in the base
context, which is what lets the job write a status on fork and Dependabot pull
requests — a `pull_request` run receives a read-only token for both, so a bot's
pull request could never be reported on and would stay blocked forever. It also
means the checker that judges a pull request is always `main`'s copy, never the
one that pull request ships. Nothing from the head is executed: the checkout
takes no `ref:`, and comment bodies reach the checker on standard input.

Bot pull requests pass automatically. A bot cannot write a record, and a
required check it can never satisfy would strand every dependency update; those
changes are still gated by every other required check.

This is a compensating control and is never called an approval.

### The gates run under the same admin

Every automated gate above is administered by the same Admin who authors the
changes. A check its author can alter is a compensating control, not separation
of duties. That cannot be argued away, so it is made detectable instead:

- The active `main` and release-tag rulesets are committed to
  `.github/rulesets/`, so weakening them appears as a reviewable diff instead
  of only in GitHub's settings UI.
- `.github/workflows/ruleset-drift.yml` exports the live rulesets weekly and
  fails on any difference from that baseline — added `bypass_actors`, changed
  `enforcement`, removed required checks. It clears the baseline directory
  before exporting, so a ruleset *deleted* from the live configuration shows up
  as a deleted file rather than as an untouched one. Exporting over the top
  would have left the strongest weakening — removing the protection
  entirely — as the single case the check could not see.
- `bypass_actors` is empty in the committed baseline, recorded whenever an
  administrator runs the export with their own token. Reading it needs
  Administration **write**, so no credential capable of it is stored in Actions:
  one would be able to delete every ruleset here, which is a bypass mechanism
  kept in order to check for bypass mechanisms.
- The field therefore has three states — `verified-empty`, `verified-nonempty`,
  and `unverifiable` — and the weekly job holds the third one explicitly. It
  compares a rendering that omits `bypass_actors` from both sides, prints the
  last administrator-verified value with the commit that recorded it, and still
  requires every other field, so a response degraded in any other way fails
  rather than narrowing the comparison. Naming the state is the point: "the
  caller could not see it" and "it is empty" are different claims, and treating
  them as one is the defect this whole area exists to avoid.
- The same export asserts `current_user_can_bypass` is `never` for the identity
  it runs as. That field is per-viewer, so it is checked on every run rather
  than committed to the baseline.
- The export is validated before any comparison is made. A token without
  **write** access to the ruleset receives `bypass_actors` *omitted* rather than
  refused — GitHub will not show who can bypass a rule to a caller who could not
  also change it — and an omitted key is indistinguishable from an empty one to
  anything comparing values. That is not a hypothetical: it is what this job
  reported as drift on its first real run. `scripts/ruleset-baseline.sh` now
  fails with a token diagnosis and prints no diff, because a diff is what
  invites someone to commit the degraded export.
- `make verify-ruleset-baseline` runs on every pull request, inside the job
  that feeds `CI Required`. The drift job is weekly and never sees a pull
  request, so this is what catches a degraded baseline on the way in.

What this does not catch, in layers. The weekly job compares live against
committed, so a baseline degraded on its own is caught the following Monday.
The pull-request check catches it at merge time. Neither stops a single change
that removes the pull-request check, commits the degraded baseline, and
disables the drift workflow together — nor one Admin changing the live
configuration and the baseline in the same act, which produces no diff at all.
Those are design limits, stated rather than mitigated. What drift detection
buys is that a weakening cannot happen *unnoticed* or *unrecorded* — the
baseline commit is in Git history either way. It does not stop a deliberate
co-ordinated change. This repository sits under a personal account, so there is
no organization or enterprise ruleset layer above the repository to appeal to.

Because `bypass_actors` is empty and the export asserts the maintainer cannot
bypass, a required check that becomes permanently unreportable locks the owner
out of `main` as well. That includes a guard added to an already-required job:
if `verify-ruleset-baseline` ever fails wrongly, the pull request fixing it
cannot merge either. Recovery is the same in both cases — set the ruleset's
`enforcement` to `evaluate`, fix the cause, and return it to `active`,
recording the temporary change in the baseline commit. An `enforcement` change
with no such record is drift.

### Review date, and the path off this mode

**2027-02-16, or the day a second Admin joins, whichever comes first.** Joining
means holding repository Admin, being listed in `MAINTAINERS.md` and
`.github/CODEOWNERS`, and having completed onboarding — the permission alone is
not the event.

That date is a statement of when this gets reconsidered, not a switch that
trips. One-person operation is the intended model right now, not a deviation
running out of time.

When a second Admin does arrive, the move is mechanical: apply
`scripts/configure-github-ruleset.sh --apply --require-approvals`, refresh the
baseline, and update the governance disclosure the release emits. ADR-0016 §6
lists the steps.

## Release Control

Only an annotated semantic-version tag reachable from `main` starts the
release workflow. It builds the pinned container image, publishes an immutable
digest, generates an SBOM and release manifest, and requests GitHub artifact
provenance attestation.

There is one channel, `vX.Y.Z`. Pre-release identifiers are rejected rather
than published on a second channel: what a release asserts is stated on the
release itself — in `release-manifest.json`, in the image labels, and in the
release notes header — instead of being encoded in a tag suffix.

The release is self-completable. Nothing in it waits on a second person, and
the disclosure it publishes is what keeps that honest. See
[Single-Maintainer Operating Mode](#single-maintainer-operating-mode) and the
[Release Checklist](./release-checklist.md).

## Accepted Historical Dispositions

- History reachable from `main` is not rewritten and no force-push remediation
  is used against it. An open pull request branch may still be rebased to
  correct commit metadata before merge.
- The large historical implementation commit is retained for provenance.
- No release tag is fabricated before the first release criteria are met.
- Historical changes without issue references remain historical evidence;
  the traceability gate applies to new pull requests and commits.
