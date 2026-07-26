---
title: Release Checklist
---

# Release Checklist

Complete this checklist for every production release. Store links to the
public issue, pull request, workflow runs, GitHub Ruleset API response, and
release artifacts. A checked box must point to evidence; the checklist is not
evidence by itself.

## Which sections apply

Two tag shapes share one release workflow, and they differ only in what the tag
promises.

| Section | Production release (`vX.Y.Z`) | Pre-release (`vX.Y.Z-rc.N`) |
|---|---|---|
| Governance and People | Required | Not applicable |
| Verification and Security | Required | Required |
| Operational Evidence | Required | Not applicable |
| Version and Provenance | Required | Required |

A pre-release exists so the software can be evaluated from a published,
attested image while the governance and operational controls are still being
established. It is built, signed, attested, and published exactly like a
production release — the artifacts are not weaker — and it is marked
`--prerelease` on GitHub so it never appears as the latest release.

What a pre-release does not carry is an assertion that this repository can
operate a production release: no independent approver, no restore exercise
record, no vulnerability-response exercise record. It is therefore not suitable
for production use, and [Container Images](../operations/container-images.md)
states that boundary for operators.

Deciding that a pre-release is good enough to deploy is the operator's
assessment to make against their own regulatory obligations. Nothing below is
waived for a production release because a pre-release of the same code went out
first.

## Governance and People

- [ ] A named backup maintainer is listed in `MAINTAINERS.md` and has completed
  the onboarding checklist.
- [ ] An independent release approver is identified and is not the tag creator
  or deployer.
- [ ] The active `main` and release-tag Rulesets were exported and reviewed;
  bypass actors, force pushes, tag deletion, and unreviewed merges are blocked.
- [ ] The release issue links every included PR, and every included PR passed
  Traceability, DCO, independent review, and CODEOWNERS review.

## Verification and Security

- [ ] The exact release commit has three consecutive successful runs of
  `CI Required` and `Security Required`; reruns are linked with attempt
  numbers and no source change between them.
- [ ] `make lint`, `make test`, `make docs-check`, PostgreSQL migration replay,
  release-image dry-run, vulnerability scans, license checks, and SBOM jobs
  passed on that commit.
- [ ] Open critical or high vulnerabilities and audit findings were resolved,
  or a time-bounded exception was independently approved under policy.
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
  time, and follow-up actions. The security owner and independent observer
  approved it.
- [ ] Migration rollback, database compatibility, encryption-key recovery,
  engine configuration digests, alert queues, and monitoring readiness were
  reviewed for this release.

## Version and Provenance

- [ ] The release version is strict SemVer and uses an annotated, protected tag
  reachable from `main`. The tag message links the release issue and approval.
- [ ] The release workflow published the container by immutable digest; no
  mutable `latest` tag is treated as release identity.
- [ ] `release-manifest.json`, `SHA256SUMS`, and the CycloneDX image SBOM are
  attached to the GitHub release and identify the tag, commit, image, and
  digest.
- [ ] GitHub verifies the artifact provenance attestation for the published
  image digest, and verification output is linked from the release issue.
- [ ] The deployment record identifies the same image digest and includes
  post-deployment health, audit, and representative workflow checks.

The repository currently has one active maintainer. Until the first four
governance and people controls above are evidenced, production release remains
blocked even if the release workflow is technically runnable. That block is on
the `vX.Y.Z` channel only; pre-release tags are what the project publishes in
the meantime.
