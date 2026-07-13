---
title: Upgrade Runbook
---

# Upgrade Runbook

## Before upgrading

1. Read the release notes and back up the PostgreSQL database and encryption-key material.
2. Test the upgrade in an environment containing a representative copy of production configuration and data.
3. Record the current application version and Engine configuration digests.

## Apply migrations

Set `MERLON_MIGRATION_DATABASE_URL` to a dedicated schema-owner connection and
run:

```bash
make migrate
```

The migration runner records every filename and checksum in
`schema_migrations`, takes an advisory lock, and applies each file in its own
transaction. A second run is a no-op; a checksum mismatch stops the rollout.
For a database that predates the ledger, set
`MERLON_MIGRATION_BASELINE=<last-applied-filename>` explicitly after verifying
the backup. The runner never infers a baseline from table contents.

## Rollback

SQL migrations are forward-only unless a release-specific rollback is supplied. If validation fails, stop the rollout, restore the pre-upgrade backup, and investigate before retrying. Do not delete migration history or edit an already-applied migration.
