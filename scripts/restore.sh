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
# Everything that can reject the restore runs before pg_restore does, including
# the serving-role preconditions that audit-hardening.sql enforces afterwards.
# A failure after the archive is loaded leaves a restored but ungranted
# database and no printed recovery steps, which is the worst state to be in
# mid-incident. The dump is also checked against the sha256 in its manifest
# when one is present.
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
# Needed to check the dump against the checksum backup.sh recorded. Checked
# here with the other tools rather than at the point of use, so a missing
# coreutils (macOS ships shasum instead) is reported before anything else.
if ! command -v sha256sum >/dev/null 2>&1; then
  echo "sha256sum not found. Install coreutils, or run this script inside a" >&2
  echo "container that has it." >&2
  exit 1
fi

if [[ ${MERLON_ENV:-} == production && $force != true ]]; then
  echo "MERLON_ENV=production. Refusing to restore without --force." >&2
  exit 1
fi

# The sibling artifacts are named after the same timestamp as the dump. Derive
# their names from the dump's BASENAME: substituting on the whole path would
# rewrite the first "merlon-db-" anywhere in it, so a dump under a directory
# like backups/merlon-db-archive/ would mangle the directory and leave the
# filename untouched.
dump_dir=$(cd -- "$(dirname -- "$dump_file")" && pwd)
dump_base=$(basename -- "$dump_file")

# sibling <replacement-prefix> <extension>
sibling() {
  local renamed=${dump_base/#merlon-db-/$1}
  printf '%s/%s%s' "$dump_dir" "${renamed%.dump}" "$2"
}

manifest_file=
keys_file=
if [[ $dump_base == merlon-db-*.dump ]]; then
  manifest_file=$(sibling merlon-backup- .json)
  keys_file=$(sibling merlon-keyring- .env)
fi

# backup.sh publishes the manifest last, so its presence is what marks a set of
# artifacts as a complete backup, and it carries the dump's checksum. pg_restore
# accepts any structurally valid custom archive, so without this a truncated
# copy, bit rot, or the wrong file out of a directory of similar names restores
# silently into a database that no longer matches the backup.
if [[ -z $manifest_file ]]; then
  echo "warning: '$dump_base' does not follow the merlon-db-<timestamp>.dump" >&2
  echo "         naming, so the manifest and key ring cannot be located." >&2
  echo "         The dump's integrity cannot be verified, and encrypted customer" >&2
  echo "         attributes stay unreadable until you load the matching key ring" >&2
  echo "         into MERLON_ENCRYPTION_KEY_RING yourself." >&2
elif [[ -f $manifest_file ]]; then
  manifest_version=$(sed -n \
    's/.*"schema_version"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' \
    "$manifest_file")
  if [[ $manifest_version != 1 ]]; then
    echo "unsupported backup manifest schema_version '${manifest_version:-none}' in $manifest_file" >&2
    echo "this script can only verify schema_version 1; refusing to restore an unverifiable dump" >&2
    exit 1
  fi
  # Scoped to the "database" object: the manifest also carries the key ring's
  # checksum, and comparing the dump against that one would always fail.
  manifest_sha=$(sed -n '/"database"[[:space:]]*:/,/}/p' "$manifest_file" \
    | sed -n 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p')
  if [[ ! $manifest_sha =~ ^[0-9a-f]{64}$ ]]; then
    echo "could not read the database checksum from $manifest_file" >&2
    exit 1
  fi
  echo "Verifying dump against $manifest_file"
  dump_sha=$(sha256sum "$dump_file" | cut -d ' ' -f 1)
  if [[ $dump_sha != "$manifest_sha" ]]; then
    echo "checksum mismatch: this dump is not the one the manifest describes" >&2
    echo "  manifest: $manifest_sha" >&2
    echo "  dump:     $dump_sha" >&2
    exit 1
  fi
  echo "Checksum matches."
else
  # Not fatal: dumps taken before manifests existed, and dumps moved on their
  # own, are still restorable. Failing here would block a legitimate recovery.
  echo "warning: no backup manifest found next to this dump" >&2
  echo "         (expected $manifest_file)" >&2
  echo "         The dump's integrity cannot be verified before restoring." >&2
fi

# Finding the key ring here is the last cheap moment to notice it is missing —
# after the restore, the database looks fine and the customer attributes
# silently do not.
if [[ -n $keys_file ]]; then
  if [[ -f $keys_file ]]; then
    echo "Matching key ring found: $keys_file"
    echo "Load it into MERLON_ENCRYPTION_KEY_RING before starting the API."
  else
    echo "warning: no matching key ring found next to this dump" >&2
    echo "         (expected $keys_file)" >&2
    echo "         Encrypted customer attributes will not be readable without it." >&2
  fi
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

# The restore is not finished when pg_restore succeeds: audit-hardening.sql
# still has to reapply the serving-role grants, and it hard-fails on role-level
# preconditions that have nothing to do with the dump. Checking them here, on a
# database that does not yet have the restored tables, is the difference
# between "this cannot work, fix the role" and a database that is restored,
# ungranted, and mid-incident with the post-restore checklist never printed.
#
# Mirrors the four exceptions in docs/operations/audit-hardening.sql that a
# fresh target can already violate (:67, :70, :76, :90). The later ones are
# about application tables and cannot be evaluated before they exist.
#
# The role subquery yields NULL rather than an error when the role is missing,
# so the checks below stay safe whatever order the planner evaluates them in.
#
# The SQL goes in on stdin rather than --command because psql interpolates
# :'app_role' only when reading a file or standard input. Interpolation is what
# keeps a role name containing quotes or semicolons from being pasted into the
# statement, so substituting it into a --command string is not an option.
app_role_ready=$(psql --dbname="$MERLON_MIGRATION_DATABASE_URL" \
  --no-psqlrc --tuples-only --no-align --set ON_ERROR_STOP=1 \
  --set "app_role=$app_role" <<'SQL'
WITH app AS (
  SELECT rolname, rolsuper
    FROM pg_catalog.pg_roles
   WHERE rolname = :'app_role'
)
SELECT CASE
  WHEN NOT EXISTS (SELECT 1 FROM app) THEN 'missing'
  WHEN (SELECT rolsuper FROM app) THEN 'superuser'
  WHEN has_database_privilege(
         (SELECT rolname FROM app), current_database(), 'CREATE')
    THEN 'database-create'
  WHEN NOT has_database_privilege(
         (SELECT rolname FROM app), current_database(), 'CONNECT')
   AND current_user <> (
         SELECT owner_role.rolname
           FROM pg_catalog.pg_database AS db
           JOIN pg_catalog.pg_roles AS owner_role ON owner_role.oid = db.datdba
          WHERE db.datname = current_database())
   AND NOT (SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname = current_user)
    THEN 'cannot-grant-connect'
  ELSE 'ok' END AS "MERLON_APP_ROLE_READY"
SQL
)

case "$app_role_ready" in
  ok) ;;
  missing)
    echo "MERLON_APP_ROLE '$app_role' does not exist on the target." >&2
    echo "Create the serving role before restoring; audit-hardening.sql cannot grant to a role that is not there." >&2
    exit 1 ;;
  superuser)
    echo "MERLON_APP_ROLE '$app_role' must not be a superuser." >&2
    echo "A superuser serving role defeats the audit hardening entirely. Use a dedicated non-superuser role." >&2
    exit 1 ;;
  database-create)
    echo "MERLON_APP_ROLE '$app_role' has forbidden CREATE on the target database." >&2
    echo "REVOKE CREATE ON DATABASE ... FROM '$app_role' (and from any role it inherits) before restoring." >&2
    exit 1 ;;
  cannot-grant-connect)
    echo "MERLON_APP_ROLE '$app_role' lacks CONNECT on the target database, and the restore role can neither grant it nor is the database owner." >&2
    echo "Have the database owner GRANT CONNECT ON DATABASE ... TO '$app_role' before restoring." >&2
    exit 1 ;;
  *)
    echo "could not verify the MERLON_APP_ROLE preconditions (got '$app_role_ready')" >&2
    exit 1 ;;
esac

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
