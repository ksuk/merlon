---
title: Release Checklist
---

# Release Checklist

Complete this checklist for every release, on whichever channel is active.
Store links to the public issue, pull request, workflow runs, GitHub Ruleset
API response, and release artifacts. A checked box must point to evidence; the
checklist is not evidence by itself.

## One channel

There is one release channel, `vX.Y.Z`. The release workflow's SemVer gate
rejects pre-release identifiers, so there is no second channel to route a
weaker claim to. What a release does and does not assert is published with the
release instead — see [Disclosed, not asserted](#disclosed-not-asserted).

Release notes come from `CHANGELOG.md`. The tag needs its own `## [X.Y.Z]`
section and the workflow refuses to publish without one; `scripts/changelog.mjs`
never falls back to `## [Unreleased]` or to a neighbouring version. The section
has to be non-empty.

Every release is completable by the maintainer alone. Nothing below waits on a
second person. That is a deliberate position, not a gap being worked around,
and the next section is how the project stays honest about it.

## Disclosed, not asserted

A `vX.Y.Z` tag from this repository asserts that every automated gate passed on
the release commit and that a self-review record exists for the changes it
contains. It does **not** assert independent approval or separation of duties,
because a repository with one maintainer cannot produce either.

That is not left to this page. The release workflow emits it with the artifact:

| Output | Carries |
|---|---|
| `release-manifest.json` | `governance: { mode, independent_approval, separation_of_duties, adr }` at `schema_version: 2` |
| Container image labels | `io.github.ksuk.merlon.governance.*`, the same four facts |
| GitHub release notes | A disclosure header above the changelog section |

An operator running a vendor assessment can therefore answer the question from
`docker inspect` or the manifest, without reading any documentation. Whether
that is good enough to deploy is their assessment to make against their own
regulatory obligations — and for an AML/CFT system, one they must be able to
defend to a regulator regardless of what any vendor claims.

ADR-0016 records the decision, the rejected alternatives, and the tailoring
against the internal quality standard.

## Verification and Security

- [ ] The exact release commit has three consecutive successful runs of
  `CI Required` and `Security Required`; reruns are linked with attempt
  numbers and no source change between them.
- [ ] `make lint`, `make test`, `make docs-check`, PostgreSQL migration replay,
  release-image dry-run, vulnerability scans, license checks, and SBOM jobs
  passed on that commit.
- [ ] Open critical or high vulnerabilities and audit findings were resolved,
  or a time-bounded exception was recorded under policy with its expiry and
  remediation. The exception is not independently approved; the record says so.
- [ ] Official runtime support and EOL sources in
  [Dependency Lifecycle](../operations/dependency-lifecycle.md) were reviewed
  again on the release date.

## Operational Evidence

- [ ] A sanitized restore exercise record identifies the backup, source and
  target PostgreSQL versions, release commit, operators, start/end timestamps,
  recovery-time result, migration ledger, health checks, representative
  encrypted reads, and `merlon-audit verify` result. Secrets and customer data
  are excluded.
- [ ] A sanitized vulnerability-response exercise record identifies the test
  advisory, detection time, owner, severity decision, affected-component
  analysis, containment, patch/exception decision, communications, elapsed
  time, and follow-up actions. The security owner signed it off; the record
  states that no independent observer was involved.
- [ ] Migration rollback, database compatibility, encryption-key recovery,
  engine configuration digests, alert queues, and monitoring readiness were
  reviewed for this release.

## Version and Provenance

- [ ] The release version is strict SemVer with no pre-release identifier, and
  uses an annotated, protected tag reachable from `main`. The tag message links
  the release issue.
- [ ] `CHANGELOG.md` describes what this tag ships. Confirm which section the
  workflow will publish with `node scripts/changelog.mjs <tag>` before tagging.
- [ ] The release workflow published the container by immutable digest; no
  mutable `latest` tag is treated as release identity.
- [ ] `release-manifest.json`, `SHA256SUMS`, and the CycloneDX image SBOM are
  attached to the GitHub release and identify the tag, commit, image, and
  digest.
- [ ] GitHub verifies the artifact provenance attestation for the published
  image digest, and verification output is linked from the release issue.
- [ ] The deployment record identifies the same image digest and includes
  post-deployment health, audit, and representative workflow checks.

## Change Provenance

- [ ] The release issue links every pull request included in this tag.
- [ ] Every included pull request passed Traceability, DCO, and
  `Governance Required` — that last one is the self-review record standing in
  for a review nobody was available to give.
- [ ] The active `main` and release-tag rulesets match the baseline committed
  in `.github/rulesets/`, and the latest Ruleset Drift run is green.

Nothing on this page is waived, deferred, or blocked on a second person. If a
box cannot be checked, the release does not go out — which is the only version
of this checklist worth having.
