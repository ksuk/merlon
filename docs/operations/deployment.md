---
title: Deployment Runbook
---

# Deployment Runbook

## Scope

The repository Docker Compose files are development/demo topologies. They are not production hardening guides. Production deployments require managed secrets, TLS termination, backup/restore procedures, network segmentation, and organization-specific regulatory controls.

## Required production controls

- Set `MERLON_ENV=production` and enable authentication.
- Terminate external TLS at a trusted ingress or reverse proxy.
- Store database passwords, bootstrap tokens, JWT material, and encryption keys in a secret manager; do not commit them or place production values in `.env` files.
- Keep `MERLON_SEED=false` outside local development.
- Restrict PostgreSQL and API `/metrics` to private networks or authenticated monitoring infrastructure.
- Back up `MERLON_ENCRYPTION_KEY_RING` and verify that recovery procedures can restore both database data and required key material.
- Run `make migrate` with `MERLON_MIGRATION_DATABASE_URL` before starting the API. The serving role must not own application tables.

## Configuration verification

Before rollout, record the native engine configuration digests from startup logs and retain the approved rule files with the release evidence. See ADR-0012.

## Audit-log database roles

The application database role should not have `UPDATE` or `DELETE` privileges on
`audit_logs`, and must not own that table. Apply
`docs/operations/audit-hardening.sql` as the migration owner, then verify the
preflight query before rollout. The API refuses to start in production when
the check fails. Grant read-only audit access through a separate role and
retain the verification output with deployment evidence.
