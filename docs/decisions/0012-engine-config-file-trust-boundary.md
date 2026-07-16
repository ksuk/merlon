# ADR-0012: Engine Configuration File Trust Boundary

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-11 |
| Related ADRs | ADR-0004, ADR-0014 |

## Context

The native Go engine loads CDD weights, transaction-monitoring scenarios, and screening lists directly from operator-supplied files. Those files are outside the database-backed `rule_definitions` audit trail. Treating them as if they were database-managed rules would create an incorrect audit claim.

## Decision

Configuration files loaded by the engine remain an operator-managed trust boundary. At startup, the engine emits deterministic SHA-256 digests for each configured file or YAML directory. The digest set identifies the exact configuration content used by the running process without exposing local filesystem paths.

## Consequences

Deploying organizations must apply source control, change approval, access control, backup, and deployment controls to engine configuration files. The startup logs and runtime digest support post-hoc verification; they do not prevent unauthorized file modification. Centralizing engine configuration in the database is a future architectural change and is not provided by this release.

This decision applies only to operator-supplied configuration files. Database-backed rules created through the rules API are inactive by default and use the atomic maker-checker control and approval ledger defined in ADR-0014. File-based engine configuration remains outside that database control, so deploying organizations must enforce independent author and deployer roles through IAM and their documented change-management process.
