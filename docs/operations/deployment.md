---
title: Deployment Runbook
---

# Deployment Runbook

## Scope

The repository Docker Compose files are development/demo topologies. They are not production hardening guides. Production deployments require managed secrets, TLS termination, backup/restore procedures, network segmentation, and organization-specific regulatory controls.

## Required production controls

- Set `MERLON_ENV=production` and enable authentication.
- Terminate external TLS at a trusted ingress or reverse proxy. Enforce
  deployment-wide rate limits there; the API's in-memory limiter is only a
  per-process defense-in-depth control.
- Have the ingress overwrite or safely append `X-Forwarded-For`, and set
  `MERLON_TRUSTED_PROXY_CIDRS` to only its narrow egress ranges so audit
  records and any application-level limiter use the observed client address.
- Store database passwords, bootstrap tokens, JWT material, and encryption keys in a secret manager; do not commit them or place production values in `.env` files.
- Keep `MERLON_SEED=false` outside local development.
- Restrict PostgreSQL and API `/metrics` to private networks or authenticated monitoring infrastructure.
- Back up `MERLON_ENCRYPTION_KEY_RING` and verify that recovery procedures can restore both database data and required key material.
- Run `make migrate`, then `make audit-harden`, with
  `MERLON_MIGRATION_DATABASE_URL` before starting the API. The serving role
  must not own application tables or hold database-level `CREATE`.

## Configuration verification

Before rollout, record the native engine configuration digests from startup logs and retain the approved rule files with the release evidence. See ADR-0012.

## Application database roles

Apply `docs/operations/audit-hardening.sql` as the migration owner after
migrations. It normalizes the serving role to CRUD on ordinary application
tables, `SELECT`/`INSERT` on append-only audit evidence, and no access to the
migration ledger or schema DDL. It rejects database-level `CREATE` and also
fails closed if inherited role membership supplies a forbidden privilege. The
serving role must not own application tables.

The API refuses to start in production when its audit-log preflight fails.
Grant read-only audit access through a separate, organization-managed role;
the serving-role procedure does not create or restore that role. Verify both
sets of grants and retain the output with deployment evidence.
