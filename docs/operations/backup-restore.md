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
export MERLON_BACKUP_DATABASE_URL='postgres://merlon_backup:...@host:5432/merlon'
export MERLON_ENCRYPTION_KEY_RING='...'
make backup            # or: scripts/backup.sh [output-directory]
```

This writes three files:

| File | Contents |
|---|---|
| `merlon-db-<timestamp>.dump` | `pg_dump` custom-format database dump |
| `merlon-keyring-<timestamp>.env` | The key ring, mode `0600` |
| `merlon-backup-<timestamp>.json` | Manifest: timestamps and SHA-256 of both |

The whole-database dump includes operator-only objects such as the migration
ledger and sequence state. `MERLON_BACKUP_DATABASE_URL` must therefore use a
dedicated read-only backup role with access to every existing and future table
and sequence. The script deliberately does not fall back to the serving role
or the DDL-capable migration owner.

### Provisioning the backup role

Create the credential through controlled secret-management tooling, and run
the following with the administrator/object-owner responsibilities shown,
replacing the database and role names when necessary:

```sql
-- Run as a database administrator; configure authentication separately.
CREATE ROLE merlon_backup
  LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE
  NOREPLICATION NOBYPASSRLS;

-- Run the database grant as the database owner/administrator.
GRANT CONNECT ON DATABASE merlon TO merlon_backup;

-- Run schema/object/default grants as merlon_migrate (the object owner).
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON SCHEMA public FROM merlon_backup;
GRANT USAGE ON SCHEMA public TO merlon_backup;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM merlon_backup;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO merlon_backup;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM merlon_backup;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO merlon_backup;

ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  REVOKE ALL PRIVILEGES ON SCHEMAS FROM merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  GRANT USAGE ON SCHEMAS TO merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  REVOKE ALL PRIVILEGES ON TABLES FROM merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  GRANT SELECT ON TABLES TO merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  REVOKE ALL PRIVILEGES ON SEQUENCES FROM merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  GRANT SELECT ON SEQUENCES TO merlon_backup;
```

Default privileges are scoped to the current database and the exact
object-creating role; membership in a role does not apply that role's defaults.
Repeat all six `ALTER DEFAULT PRIVILEGES` statements in every target database
and for every role that creates objects, and repeat the direct normalization
for any supported application schema beyond `public`. Rerun the existing-object
grants after introducing the role and after every restore.

Do not grant table `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `TRIGGER`,
`REFERENCES`, or PostgreSQL 18 `MAINTAIN`; sequence `USAGE` or `UPDATE`; schema
`CREATE`; role membership; or ownership. If row-level security is introduced,
test a complete dump explicitly and review the narrowly necessary RLS policy
instead of silently granting broad ownership or DDL capability.

Merlon does not create or support PostgreSQL large objects in this logical
backup. The script queries `pg_largeobject_metadata` and refuses to create
backup artifacts if any are present, before invoking `pg_dump`. Move
organization-managed large objects to a separately controlled backup path.
Also test every added schema and RLS policy whenever the database object model
changes.

The script **refuses to run** when `MERLON_ENCRYPTION_KEY_RING` is unset,
rather than quietly producing a database-only backup. Pass `--no-keys` only if
the deployment genuinely stores no encrypted attributes; that is never correct
for a production database.

`BACKUP_DIR` selects the output directory (`make backup BACKUP_DIR=/mnt/backups`),
defaulting to `backups/`. The script enforces mode `0700` on this directory and
creates the dump, key-ring file, and manifest without group or other access.
It writes hidden temporary files in that directory and publishes the manifest
last; if `pg_dump` fails, the temporary dump is removed and no final-name
backup is left behind.

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

Have a database administrator create the isolated target with the restore role
as owner.

```sql
CREATE DATABASE merlon_recovery OWNER merlon_migrate TEMPLATE template0;
```

If policy requires a different database owner, a DBA must explicitly transfer
the fresh database's `public` schema to the restore role. A `CREATE` grant on
the schema alone is not sufficient. PostgreSQL requires the prospective schema
owner to have database-level `CREATE` during the ownership transfer; revoke
that temporary database privilege immediately afterward. Also pre-grant direct
database `CONNECT` to both roles because the hardening procedure cannot create
that grant when its executor is not the database owner or a superuser.

```sql
CREATE DATABASE merlon_recovery OWNER platform_db_owner TEMPLATE template0;
GRANT CONNECT, CREATE ON DATABASE merlon_recovery TO merlon_migrate;
\connect merlon_recovery
ALTER SCHEMA public OWNER TO merlon_migrate;
REVOKE CREATE ON DATABASE merlon_recovery FROM merlon_migrate;
GRANT CONNECT ON DATABASE merlon_recovery TO merlon_app;
```

```bash
export MERLON_MIGRATION_DATABASE_URL='postgres://merlon_migrate:...@host:5432/merlon_recovery'
export MERLON_APP_ROLE='merlon_app' # optional; this is the default
make restore BACKUP_FILE=backups/merlon-db-20260726T090000Z.dump

# Production requires an additional explicit acknowledgement:
MERLON_ENV=production make restore \
  BACKUP_FILE=backups/merlon-db-20260726T090000Z.dump \
  RESTORE_FORCE=true
```

This entry point deliberately does not perform an in-place restore. Create a
fresh, isolated target database and point `MERLON_MIGRATION_DATABASE_URL` at
it. The preflight refuses public relations, routines, or types; extra
non-system schemas; non-default extensions; and PostgreSQL large objects.
These checks cover every object kind created by Merlon migrations.
Consequently, restoring an old archive over a newer Merlon schema cannot leave
newer objects behind: the nonempty target is rejected before the prompt or any
modification. Organization-defined collations, conversions, operators,
text-search objects, publications, subscriptions, or event triggers are
outside this preflight and must not be pre-created in the isolated target.

The restore connection must use the target object-owner role supplied through
`MERLON_MIGRATION_DATABASE_URL`; the least-privilege `merlon_app` serving role
cannot recreate archive objects, and the script deliberately does not fall
back to `MERLON_DATABASE_URL`. Before prompting, the script also verifies that
this role manages the `public` schema and has `CREATE` there. A fresh database
owned by another role is rejected until its owner transfers `public` as shown
above. The prompt prints the server-reported target identity and confirms that
the preflight found a fresh target. No existing schema is dropped by this
entry point.

`pg_restore` runs with `--single-transaction` and `--exit-on-error`, so an
archive error cannot commit a partially restored set of objects. A failed
restore leaves the fresh target without archive objects. Keep the API stopped,
correct the archive or permissions, and repeat against a fresh target.

The serving role named by `MERLON_APP_ROLE` must already exist. After
`pg_restore`, the script applies the idempotent serving-role procedure in
`audit-hardening.sql`: ordinary application tables receive CRUD, audit and
rule-activation evidence remains `SELECT`/`INSERT` only, the audit sequence is
usable, and schema DDL and the migration ledger remain owner-only. The archive
continues to omit ACLs deliberately, so this procedure also reconstructs ACLs
for older backups produced with `--no-privileges`.

Only the role named by `MERLON_APP_ROLE` is reconstructed automatically. The
dedicated backup role's existing-object grants and default privileges, plus
organization-specific auditor, read-only, reporting, or integration-role ACLs,
are not part of the archive or this procedure. Reapply and verify all of them
from controlled definitions before checking readiness.

Before prompting, the script connects with `psql` and prints only the target
user, server address, port, and database reported by PostgreSQL. It never
echoes the connection string, because libpq accepts forms whose passwords
cannot be safely masked by one URI substitution. Confirm that identity, then
type `restore` to proceed. The script refuses to run against
`MERLON_ENV=production` without `--force`; the Make target translates only
`RESTORE_FORCE=true` into that flag. The common catastrophic mistake is
restoring into the wrong database, not restoring the wrong backup.

It also looks for the key-ring file matching the dump's timestamp and warns if
it is absent. That is the last cheap moment to notice: after the restore the
database looks healthy and the customer attributes silently do not.

### After restoring

Restore into an isolated environment first. Then, in order:

1. Load the matching key ring into `MERLON_ENCRYPTION_KEY_RING`.
2. Apply migrations for the target release: `make migrate`. Do not alter
   historical migration files.
3. Run `make audit-harden` with the same `MERLON_MIGRATION_DATABASE_URL` and
   `MERLON_APP_ROLE`. This required, idempotent second pass grants any tables
   created by step 2; the migration ledger prevents `make migrate` from
   replaying grants for objects that were already restored.
4. Reapply and verify the dedicated backup role provisioning above, including
   existing-object grants and future default privileges, plus all
   organization-specific auditor, read-only, reporting, and integration-role
   ACLs from controlled definitions.
5. Keep the API and worker stopped. Update their secret/configuration so
   `MERLON_DATABASE_URL` names the fresh target through the serving role—not
   the migration or backup role—then start both processes.
6. Confirm readiness: `GET /healthz/ready` must report every check `ok`.
7. **Read a representative encrypted customer attribute back.**
8. Run `merlon-audit verify` and investigate any failure before treating the
   environment as recovered.

Step 7 is the one that catches a key-ring mismatch. A restore can pass every
other check and still have produced unreadable data.

## Rollback is restore

Migrations are forward-only. Rolling back a release that changed the schema
means creating a fresh database, restoring and validating the pre-upgrade
backup there, and only then cutting `MERLON_DATABASE_URL` over to that fresh
target. Never point this restore entry point at the current database. The
cutover loses everything written since the backup. See [Upgrading](upgrade.md) and
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
