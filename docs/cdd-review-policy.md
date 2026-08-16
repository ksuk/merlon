---
title: CDD Periodic Review Policy
sidebar_position: 8
---

# CDD Periodic Review Policy

CDD periodic-review timing is configuration, not a constant in the review
worker. The default file is `content/cdd_review_policy_v1.yaml`; set
`MERLON_CDD_REVIEW_POLICY_PATH` to use an institution-approved version.

The policy fixes the intervals at 365 days for High, 730 days for Medium, and
1,095 days for Low. An unscored customer is treated as High (fail-alert). The
anchor is selected in this order: the last completed review, the last CDD
score, then customer creation. A tier increase brings the next review forward
to the evaluation time. A 30-day grace period is exposed with the schedule.

Customers with no completed review or score receive a deterministic cold-start
offset derived from their ID: up to 30 days for High, 90 for Medium, and 180
for Low. The same customer, policy digest, and input always produce the same
date. Completion requires a rationale and is limited to Analyst and Admin
roles; the durable review queue and completion records are described in the
CDD review workflow documentation.

The loader rejects an incorrect schema or policy version, missing or
non-positive intervals, unknown YAML fields, an incomplete anchor list, and
unauthorized completion roles. The parsed policy's SHA-256 digest and version
are included in `GET /api/v1/system/status` and the read-only policy API so an
operator can reproduce why a review was scheduled.
