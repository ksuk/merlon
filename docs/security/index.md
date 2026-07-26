---
title: Security and Assurance
---

# Security and Assurance

Material for the people who have to sign off on running Merlon: security
reviewers, vendor-risk assessors, and internal audit.

Merlon is deployed by regulated institutions, which means the question is never
only "is this software any good" but "can we evidence that we assessed it".
These pages are written for the second question.

| Page | Answers |
|---|---|
| [Data Egress](data-egress.md) | What leaves our network, and what triggers it |
| [Supply Chain](supply-chain.md) | How dependencies, images, and releases are controlled and evidenced |
| [Accepted Risks](accepted-risks/index.md) | What the project knows it does not do, and why |

## Related material

| Where | What |
|---|---|
| [SECURITY.md](https://github.com/ksuk/merlon/blob/main/SECURITY.md) | How to report a vulnerability, and the response commitments |
| [Authorization](../auth.md) | Role model, segregation of duties, dual control |
| [Regulatory Scope](../compliance/regulatory-scope.md) | What Merlon does and does not claim to cover |
| [FSA Guideline Mapping](../compliance/fsa-guideline-mapping.md) | Coverage against the FSA AML/CFT guidelines |
| [Data Retention](../compliance/data-retention.md) | Retention policy enforcement |
| [Container Images](../operations/container-images.md) | What is published, how to verify it |
| [Release Checklist](../development/release-checklist.md) | What a release asserts, and what is not yet evidenced |

## The short version

If you are assessing Merlon and want the answers without reading further:

- **No outbound connections you did not configure.** No telemetry, no analytics,
  no licence check, no update check. See [Data Egress](data-egress.md).
- **All data stays in your PostgreSQL database**, in your infrastructure.
  Direct-PII customer attributes are encrypted at rest with keys held outside
  the database.
- **The container runs as a non-root user and needs no writable filesystem.**
- **Published images are immutable, multi-architecture, and carry a build
  provenance attestation and a CycloneDX SBOM.** There is no `latest` tag.
- **Audit records are append-only at the database privilege level**, and the
  application refuses to start in production if that is not enforced.
- **One active maintainer.** The project publishes pre-release tags and states
  plainly that production release remains gated. See the
  [release checklist](../development/release-checklist.md).

That last point is the one most vendor questionnaires do not have a field for,
and it is the one most likely to matter to your assessment.
