---
title: "Troubleshooting: Database and Migrations"
---

# Troubleshooting: Database and Migrations

Migrations are forward-only and applied by an explicit operator step
(`make migrate`), never automatically on startup. Most problems here are the
migration runner refusing to proceed, which is the intended behaviour: it stops
rather than applying a schema change it cannot account for.

Start from [Troubleshooting](index.md) if you are not sure this is the right
page.

## The migration role is missing

```
MERLON_MIGRATION_DATABASE_URL is required in production
```

Or, outside production:

```
using MERLON_DATABASE_URL as migration role; production must use a separate role
```

Schema changes and serving traffic use different roles on purpose. The serving
role has no DDL rights and cannot modify `audit_logs`; if migrations ran as the
serving role, the serving role would need exactly the privileges the audit
controls exist to deny it.

Set both, pointing at the same database with different roles:

```bash
export MERLON_DATABASE_URL='postgres://merlon_app:...@host:5432/merlon'
export MERLON_MIGRATION_DATABASE_URL='postgres://merlon_migrate:...@host:5432/merlon'
make migrate
```

Outside production the warning is not fatal, so a development shortcut will not
block you — but it means development is not exercising the privilege split that
production enforces.

## A migration checksum does not match

```
migration 021 checksum mismatch: ledger=<sha256> file=<sha256>
```

The runner records a SHA-256 of every migration it applies. This says the file
on disk now differs from the file that was applied to this database.

That is a rollout-stopping condition, and the runner is right to stop: the
database's actual schema no longer corresponds to the migration history in the
repository, so no later migration can be reasoned about.

Usual causes, in order of likelihood:

1. **An already-applied migration was edited.** Applied migrations are
   immutable. Revert the file to its applied content and put the change in a
   new migration.
2. **The deployment was pointed at a database from a different branch or
   environment.** Confirm which database `MERLON_MIGRATION_DATABASE_URL`
   actually resolves to.
3. **Line endings changed.** The checksum covers bytes. A file rewritten with
   CRLF hashes differently even though it is the same SQL.

Do not delete rows from `schema_migrations` to move past this. That does not
reconcile the schema, it only removes the evidence that it diverged.

## An existing database has no ledger

```
MERLON_MIGRATION_BASELINE "0xx_name.sql" does not match a migration filename
```

Or, on a database that predates the ledger, the runner tries to apply
migrations that are already present.

The runner never infers a baseline from table contents — guessing which
migrations "look applied" is how a schema gets silently half-applied. You state
it:

```bash
MERLON_MIGRATION_BASELINE=017_retention.sql make migrate
```

Every migration up to and including that filename is recorded as applied
without being run; everything after it is applied normally. The value must be
an exact filename from `migrations/` — run `ls migrations/` and copy one,
rather than reconstructing a name from what a table is called.

Take a backup before doing this. A wrong baseline skips real migrations, and
the ledger will then claim they ran.

## Restored data cannot be decrypted

Direct-PII customer attributes are encrypted at rest with
`MERLON_ENCRYPTION_KEY_RING`. The keys are not in the database.

**A database backup without the corresponding key ring is permanently
unreadable.** There is no recovery path — not a support path, not a vendor
path. The data is gone.

If a restored database returns errors or unreadable values on customer
attributes, the key ring does not contain the key that encrypted them. Restore
the key ring from the point in time matching the database backup.

Key rotation re-encrypts in batches without downtime, which means a backup can
predate a rotation. Retain retired keys for at least as long as you retain
backups that were written under them. See
[Backup and Restore](../operations/backup-restore.md).

## Verifying after any of the above

```bash
# Applying twice must be a no-op — the second run reports nothing to apply.
make migrate && make migrate

# The append-only audit log must still verify.
cd api && go run ./cmd/merlon-audit verify

# Readiness must be clean.
curl -s http://localhost:8080/healthz/ready
```
