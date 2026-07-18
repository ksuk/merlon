# ADR-0014: Atomic Dual Control for Rule Activation

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-16 |
| Related ADRs | ADR-0004, ADR-0012 |

## Context

Database-backed rule definitions are immutable versions. A rule author can
create a new inactive version, and an Admin can later activate it. The prior
HTTP-layer check compared the approver with the creator of the currently
active version, while the store activated the latest version. That split
allowed the latest version's author to approve their own change and also
created a check-then-act race.

The control must identify the exact version being changed, fail closed when
identity evidence is incomplete, remain correct under concurrent requests,
and leave append-only evidence without changing the existing REST response
shape.

## Decision

Rule state changes are enforced inside the repository transaction:

1. Acquire a transaction-scoped advisory lock derived from the rule name.
2. Select and lock the exact target: the latest version for activation, or the
   currently active version for deactivation.
3. Reject the request when the approver identity or creator identity is
   missing, or when they are equal.
4. Change active state and insert a `rule_activation_events` row in the same
   transaction. The row records the target version, creator, approver,
   requested state, and timestamp.
5. Grant the serving role only `SELECT` and `INSERT` on the approval ledger;
   reject unsafe ownership or `UPDATE`/`DELETE` privileges during the
   production audit preflight.

New rules and new versions created through the HTTP API are always inactive.
The memory repository implements the same target selection and fail-closed
decision so local and PostgreSQL behavior remain aligned. HTTP mutation audit
records include the target version, author, approver, requested state,
decision, outcome, and status code.

## Consequences

Activation and deactivation require two attributable Admin identities. A
legacy row whose `created_by` value is absent cannot change active state until
the deployment performs a reviewed data-remediation procedure; guessing or
backfilling an author automatically would weaken the evidence.

The approval event is written only when state actually changes. A successful
no-op request remains visible in the HTTP audit log with `changed=false`.
Rule versions remain immutable and the REST rule representation remains
backward compatible.

ADR-0012 still governs operator-supplied files loaded directly by the native
engine. Those files are not silently brought into this database-backed
control; independent author/deployer IAM and deployment evidence remain an
operator responsibility.

Rollback of application code must not drop migration 034 or its evidence.
Older binaries may ignore the table, but production release rollback must
retain its append-only privileges and restore the dual-control implementation
before any subsequent rule state change.
