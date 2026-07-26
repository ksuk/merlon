---
title: Backup and Restore Runbook
---

# Backup and Restore Runbook

A Merlon backup is **two artifacts, not one**: the PostgreSQL database and the
encryption key ring.

Direct-PII customer attributes are encrypted at rest with keys held in
`MERLON_ENCRYPTION_KEY_RING`, outside the database. A database backup without
the matching keys leaves those attributes permanently unreadable. There is no
recovery path — not a support path, not a vendor path. This is the single most
consequential fact on this page.

## Backup

```bash
export MERLON_DATABASE_URL='postgres://merlon_app:...@host:5432/merlon'
export MERLON_ENCRYPTION_KEY_RING='...'
make backup            # or: scripts/backup.sh [output-directory]
```

This writes three files:

| File | Contents |
|---|---|
| `merlon-db-<timestamp>.dump` | `pg_dump` custom-format database dump |
| `merlon-keyring-<timestamp>.env` | The key ring, mode `0600` |
| `merlon-backup-<timestamp>.json` | Manifest: timestamps and SHA-256 of both |

The script **refuses to run** when `MERLON_ENCRYPTION_KEY_RING` is unset,
rather than quietly producing a database-only backup. Pass `--no-keys` only if
the deployment genuinely stores no encrypted attributes; that is never correct
for a production database.

`BACKUP_DIR` selects the output directory (`make backup BACKUP_DIR=/mnt/backups`),
defaulting to `backups/`.

### Storing it

**Put the key ring somewhere the database backup is not.** Anyone holding both
files holds the plaintext customer data; anyone holding neither cannot restore.
Storing them together converts encryption-at-rest into a filing convention.

Retain retired keys for at least as long as you retain backups written under
them. Key rotation re-encrypts online and in batches, so a backup can easily
predate a rotation — discard the old key and that backup becomes unreadable
without anything appearing to have gone wrong.

Do not commit either file. Apply encryption, access controls, and retention
appropriate to the data classification, and prefer physical backups
(`pg_basebackup`, WAL archiving) where your recovery objectives require them.

## Restore

```bash
export MERLON_DATABASE_URL='postgres://merlon_app:...@host:5432/merlon'
make restore BACKUP_FILE=backups/merlon-db-20260726T090000Z.dump
```

This is destructive: it drops and recreates objects in the target database. The
script prints the target with the password redacted and requires you to type
`restore` to proceed, and it refuses to run against `MERLON_ENV=production`
without `--force` — the common catastrophic mistake is restoring into the wrong
database, not restoring the wrong backup.

It also looks for the key-ring file matching the dump's timestamp and warns if
it is absent. That is the last cheap moment to notice: after the restore the
database looks healthy and the customer attributes silently do not.

### After restoring

Restore into an isolated environment first. Then, in order:

1. Load the matching key ring into `MERLON_ENCRYPTION_KEY_RING`.
2. Apply migrations for the target release: `make migrate`. Do not alter
   historical migration files.
3. Confirm readiness: `GET /healthz/ready` must report every check `ok`.
4. **Read a representative encrypted customer attribute back.**
5. Run `merlon-audit verify` and investigate any failure before treating the
   environment as recovered.

Step 4 is the one that catches a key-ring mismatch. A restore can pass every
other check and still have produced unreadable data.

## Rollback is restore

Migrations are forward-only. Rolling back a release that changed the schema
means restoring the pre-upgrade backup, which loses everything written since.
See [Upgrading](upgrade.md) and
[Accepted Risks](../security/accepted-risks/index.md).

The practical consequence: your backup interval is your maximum rollback data
loss. Choose it accordingly, not by convention.

## Recovery evidence

Record the backup identifier, restore operator, application version, native
engine configuration digests, validation results, and any exceptions in the
organization's change-management system.

Before the first production release and at least annually thereafter, perform
an isolated restore exercise. The sanitized record must identify the source
and target PostgreSQL versions, release commit and image digest, operators,
start and completion timestamps, recovery-time result, schema migration ledger,
health checks, representative encrypted reads, and `merlon-audit verify`
result. Have an independent observer approve the record.

Never place credentials, encryption keys, backup locations, or customer data in
the public repository.

An untested backup is not a rollback plan. The exercise is what turns this
runbook into a control.
