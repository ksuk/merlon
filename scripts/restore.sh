#!/usr/bin/env bash
set -euo pipefail

# Restore a Merlon backup produced by scripts/backup.sh.
#
# This only restores into a fresh target database; it refuses an in-place
# restore over user objects, extra schemas, or extensions. It also requires an
# explicit confirmation and refuses MERLON_ENV=production unless --force is
# given, because the common catastrophic mistake is targeting the wrong
# database rather than selecting the wrong backup.
#
# Restoring the database is only half the job. Encrypted customer attributes
# stay unreadable until the matching key ring is in place, so this script
# reminds you which key-ring file belongs to the dump and warns if it is
# missing.
#
# Usage:
#   MERLON_MIGRATION_DATABASE_URL=postgres://... \
#     scripts/restore.sh <backup.dump> [--force]
#
# Environment:
#   MERLON_MIGRATION_DATABASE_URL  required restore/object-owner connection
#   MERLON_APP_ROLE                 serving role to re-grant (default merlon_app)

usage() {
  sed -n '3,23p' "$0" | sed 's/^# \{0,1\}//'
}

force=false
dump_file=

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) force=true; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    *)
      if [[ -n $dump_file ]]; then
        echo "unexpected argument: $1" >&2
        exit 2
      fi
      dump_file=$1
      shift
      ;;
  esac
done

if [[ -z $dump_file ]]; then
  echo "a backup file is required" >&2
  usage >&2
  exit 2
fi

if [[ ! -f $dump_file ]]; then
  echo "no such file: $dump_file" >&2
  exit 1
fi

if [[ -z ${MERLON_MIGRATION_DATABASE_URL:-} ]]; then
  echo "MERLON_MIGRATION_DATABASE_URL is required" >&2
  exit 1
fi
app_role=${MERLON_APP_ROLE:-merlon_app}
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
role_grants_file=$repo_root/docs/operations/audit-hardening.sql
if [[ ! -f $role_grants_file ]]; then
  echo "serving-role grant procedure not found: $role_grants_file" >&2
  exit 1
fi

if ! command -v pg_restore >/dev/null 2>&1; then
  echo "pg_restore not found. Install the PostgreSQL client tools." >&2
  exit 1
fi
if ! command -v psql >/dev/null 2>&1; then
  echo "psql not found. Install the PostgreSQL client tools." >&2
  exit 1
fi

if [[ ${MERLON_ENV:-} == production && $force != true ]]; then
  echo "MERLON_ENV=production. Refusing to restore without --force." >&2
  exit 1
fi

# The key ring is named after the same timestamp as the dump. Finding it here
# is the last cheap moment to notice it is missing — after the restore, the
# database looks fine and the customer attributes silently do not.
keys_file=${dump_file/merlon-db-/merlon-keyring-}
keys_file=${keys_file%.dump}.env
if [[ -f $keys_file ]]; then
  echo "Matching key ring found: $keys_file"
  echo "Load it into MERLON_ENCRYPTION_KEY_RING before starting the API."
else
  echo "warning: no matching key ring found next to this dump" >&2
  echo "         (expected $keys_file)" >&2
  echo "         Encrypted customer attributes will not be readable without it." >&2
fi

# Ask PostgreSQL for the target identity instead of trying to redact a raw
# connection string. libpq accepts URI, keyword/value, and service-file DSNs;
# echoing any of them after ad-hoc substitution risks leaking a restore-role
# password in query parameters or keyword values.
target_identity=$(psql --dbname="$MERLON_MIGRATION_DATABASE_URL" \
  --no-psqlrc --tuples-only --no-align --set ON_ERROR_STOP=1 \
  --command "SELECT current_user || '@' ||
    COALESCE(inet_server_addr()::text, 'local') || ':' ||
    COALESCE(inet_server_port()::text, 'local') || '/' ||
    current_database()")

target_freshness=$(psql --dbname="$MERLON_MIGRATION_DATABASE_URL" \
  --no-psqlrc --tuples-only --no-align --set ON_ERROR_STOP=1 \
  --command "SELECT CASE WHEN
      EXISTS (
        SELECT 1
          FROM pg_catalog.pg_class AS c
          JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
         WHERE n.nspname = 'public'
      )
      OR EXISTS (
        SELECT 1
          FROM pg_catalog.pg_proc AS p
          JOIN pg_catalog.pg_namespace AS n ON n.oid = p.pronamespace
         WHERE n.nspname = 'public'
      )
      OR EXISTS (
        SELECT 1
          FROM pg_catalog.pg_type AS t
          JOIN pg_catalog.pg_namespace AS n ON n.oid = t.typnamespace
         WHERE n.nspname = 'public'
      )
      OR EXISTS (
        SELECT 1
         FROM pg_catalog.pg_namespace
         WHERE nspname NOT IN ('public', 'pg_catalog', 'information_schema')
           AND nspname !~ '^pg_'
      )
      OR EXISTS (
        SELECT 1
          FROM pg_catalog.pg_extension
         WHERE extname <> 'plpgsql'
      )
      OR EXISTS (
        SELECT 1
          FROM pg_catalog.pg_largeobject_metadata
      )
    THEN 'not-fresh' ELSE 'fresh' END AS \"MERLON_FRESH_TARGET\"")

if [[ $target_freshness != fresh ]]; then
  echo "fresh target database is required; refusing a target with public relations/routines/types, extra user schemas, non-default extensions, or large objects" >&2
  exit 1
fi

schema_access=$(psql --dbname="$MERLON_MIGRATION_DATABASE_URL" \
  --no-psqlrc --tuples-only --no-align --set ON_ERROR_STOP=1 \
  --command "SELECT CASE WHEN
      has_schema_privilege(current_user, 'public', 'CREATE')
      AND EXISTS (
        SELECT 1
          FROM pg_catalog.pg_namespace AS n
         WHERE n.nspname = 'public'
           AND pg_has_role(current_user, n.nspowner, 'USAGE')
      )
    THEN 'managed' ELSE 'unmanaged' END AS \"MERLON_SCHEMA_ACCESS\"")

if [[ $schema_access != managed ]]; then
  echo "restore role must own or manage the public schema and have CREATE; the database owner must run ALTER SCHEMA public OWNER TO the restore role before retrying" >&2
  exit 1
fi

cat <<EOF

About to restore
  from: $dump_file
  into: $target_identity

This entry point restores only into a fresh target database. It never resets
an existing schema in place. The preflight found no public relations, routines,
or types; extra user schemas; non-default extensions; or large objects. It
also verified that the restore role manages the public schema and can create
objects there. Archive restoration is one transaction.
EOF

read -r -p "Type the word 'restore' to continue: " confirm
if [[ $confirm != restore ]]; then
  echo "Aborted." >&2
  exit 1
fi

echo "Restoring..."
pg_restore --no-owner --no-privileges --single-transaction --exit-on-error \
  --dbname "$MERLON_MIGRATION_DATABASE_URL" "$dump_file"

echo "Reapplying serving-role grants and audit hardening..."
psql --dbname="$MERLON_MIGRATION_DATABASE_URL" \
  --no-psqlrc --set ON_ERROR_STOP=1 \
  --set "MERLON_APP_ROLE=$app_role" \
  --file "$role_grants_file"

cat <<'EOF'

Database restore and existing-schema grants complete. Before treating this
environment as recovered:

  1. Load the matching key ring into MERLON_ENCRYPTION_KEY_RING.
  2. Apply migrations for the target release:   make migrate
  3. Reapply serving-role grants:               make audit-harden
  4. Reapply dedicated backup-role grants/defaults and all organization-specific ACLs.
  5. Point MERLON_DATABASE_URL at the fresh target, then start API/worker.
  6. Check readiness:                           curl -s .../healthz/ready
  7. Read a representative encrypted customer attribute back.
  8. Verify the audit chain:                    merlon-audit verify

Step 7 is the one that catches a key-ring mismatch. Do not skip it: a restore
that passes every other check can still have produced unreadable data.
EOF
