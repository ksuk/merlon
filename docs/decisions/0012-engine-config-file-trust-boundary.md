# ADR-0012: Engine Configuration File Trust Boundary

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-11 |
| Related ADRs | ADR-0004 |

## Context

The Rust Engine loads CDD weights, transaction-monitoring scenarios, and screening lists directly from operator-supplied files. Those files are outside the database-backed `rule_definitions` audit trail. Treating them as if they were database-managed rules would create an incorrect audit claim.

## Decision

Configuration files loaded by the Engine remain an operator-managed trust boundary. At startup, the Engine emits a deterministic SHA-256 digest for each configured file or YAML directory and exposes the same digest set through ConfigService. The runtime digest identifies the exact configuration content used by the running Engine without exposing local filesystem paths through the RPC.

## Consequences

Deploying organizations must apply source control, change approval, access control, backup, and deployment controls to Engine configuration files. The startup logs and runtime-digest RPC support post-hoc verification; they do not prevent unauthorized file modification. Centralizing Engine configuration in the database is a future architectural change and is not provided by this release.

Rule creation and rule activation do not currently enforce a creator-versus-activator separation of duties in the application. Until that control is implemented, deploying organizations must enforce it through IAM roles and their documented change-management process.
