---
title: Backup and Restore Runbook
---

# Backup and Restore Runbook

## Backup

Back up PostgreSQL using the method approved for the deployment, such as `pg_dump` for logical backups or `pg_basebackup` for physical backups. Protect backups with encryption, access controls, retention policies, and restore testing appropriate to the data classification.

Back up `MERLON_ENCRYPTION_KEY_RING` separately from the database. A database backup without the corresponding keys can leave encrypted customer attributes permanently unreadable. Do not store the key ring in source control or alongside an unencrypted database backup.

## Restore

1. Restore into an isolated environment first.
2. Restore the matching encryption-key material before starting the API.
3. Apply only the migrations required by the target release; do not alter historical migration files.
4. Validate PostgreSQL connectivity, API health endpoints, and representative encrypted customer reads.
5. Run `merlon-audit verify` and investigate any verification failure before treating the environment as recovered.

## Recovery evidence

Record the backup identifier, restore operator, application version, Engine configuration digests, validation results, and any exceptions in the organization's change-management system.
