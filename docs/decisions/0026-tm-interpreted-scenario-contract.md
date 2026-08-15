# [ADR-0026] Interpreted TM scenario contract

| Item | Decision |
|---|---|
| Status | Accepted |
| Decision date | 2026-08-15 |
| Related ADRs | ADR-0004, ADR-0006, ADR-0012, ADR-0025 |

## Context

Transaction-monitoring scenario files are product configuration, but a field
that is accepted by a decoder and ignored by the evaluator is not a control.
The previous scenario loader also selected an evaluator from an identifier
substring, which made a rename change behavior and allowed an unknown rule to
look valid. The transaction vocabulary and aggregation window likewise need to
survive ingestion and be visible in the audit trail.

## Decision

The native engine exposes a small, typed interpreted contract (schema v2.1):

- every v2.1 document declares one of five detectors: `structuring`,
  `rapid_movement`, `high_frequency_small_amount`,
  `dormant_account_reactivation`, or `high_risk_country_transfer`;
- unknown top-level and condition keys fail engine construction;
- `transaction_type` is an optional canonical filter. Empty legacy values are
  interpreted from direction as `transfer_in`, `transfer_out`, or `transfer`;
- `aggregation` is restricted to `field=amount`, `group_by=customer_id`, and a
  positive Go duration. Its `sum`/`count` function is interpreted by the
  detector and its period is the evaluation window;
- the absolute safety valve is applied after a detector candidate is formed,
  with a default amount of 10,000,000 and a high-frequency count of 25;
- v1/v2.0 files remain a bounded compatibility path until 2027-08-15. They
  may infer a detector only from the existing known ID prefixes, and the
  loaded engine reports a compatibility warning in System Status. Unknown IDs
  fail closed.

The transaction API and persistence layer carry the optional canonical
`transaction_type`, and `/api/v1/system/status` reports the contract version,
supported detectors, compatibility warnings, and loaded configuration digest.

## Rationale

Five explicit bindings make the executable behavior reviewable without
introducing a generic rule DSL. Rejecting unused vocabulary protects
Auditability First and Fail-Alert: a typo cannot silently disable a detector.
The compatibility window preserves the 12-month contract-stability promise
while making technical debt visible to operators.

## Consequences

Authors must use the schema and choose a supported detector. A new detector is
a code-and-schema change with regression tests, rather than an arbitrary YAML
value. Existing v1/v2.0 deployments continue to run, but their System Status
must be monitored and their files migrated before the compatibility deadline.
