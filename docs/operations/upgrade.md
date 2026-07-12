---
title: Upgrade Runbook
---

# Upgrade Runbook

## Before upgrading

1. Read the release notes and back up the PostgreSQL database and encryption-key material.
2. Test the upgrade in an environment containing a representative copy of production configuration and data.
3. Record the current application version and Engine configuration digests.

## Apply migrations

Set `MERLON_DATABASE_URL` to the target database and run:

```bash
make migrate
```

The target applies `migrations/*.sql` in lexical order and stops on the first SQL error. It is intended for an operator workstation or deployment job that has `psql` installed. Do not use it against an unverified production backup.

## Rollback

SQL migrations are forward-only unless a release-specific rollback is supplied. If validation fails, stop the rollout, restore the pre-upgrade backup, and investigate before retrying. Do not delete migration history or edit an already-applied migration.
