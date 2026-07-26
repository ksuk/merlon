#!/usr/bin/env bash
set -euo pipefail

# Restore a Merlon backup produced by scripts/backup.sh.
#
# This is destructive: it drops and recreates the objects in the target
# database. It refuses to run without an explicit confirmation, and it refuses
# to run against MERLON_ENV=production unless --force is given, because the
# common catastrophic mistake is restoring into the wrong database rather than
# restoring the wrong backup.
#
# Restoring the database is only half the job. Encrypted customer attributes
# stay unreadable until the matching key ring is in place, so this script
# reminds you which key-ring file belongs to the dump and stops if it is
# missing.
#
# Usage:
#   MERLON_DATABASE_URL=postgres://... scripts/restore.sh <backup.dump> [--force]

usage() {
  sed -n '3,18p' "$0" | sed 's/^# \{0,1\}//'
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

if [[ -z ${MERLON_DATABASE_URL:-} ]]; then
  echo "MERLON_DATABASE_URL is required" >&2
  exit 1
fi

if ! command -v pg_restore >/dev/null 2>&1; then
  echo "pg_restore not found. Install the PostgreSQL client tools." >&2
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

# Show which database is about to be overwritten, with the password removed.
redacted=$(printf '%s' "$MERLON_DATABASE_URL" | sed -E 's#://([^:/@]+):[^@]*@#://\1:***@#')

cat <<EOF

About to restore
  from: $dump_file
  into: $redacted

This DROPS existing objects in the target database.
EOF

read -r -p "Type the word 'restore' to continue: " confirm
if [[ $confirm != restore ]]; then
  echo "Aborted." >&2
  exit 1
fi

echo "Restoring..."
pg_restore --clean --if-exists --no-owner --no-privileges \
  --dbname "$MERLON_DATABASE_URL" "$dump_file"

cat <<'EOF'

Restore complete. Before treating this environment as recovered:

  1. Load the matching key ring into MERLON_ENCRYPTION_KEY_RING.
  2. Apply migrations for the target release:   make migrate
  3. Check readiness:                           curl -s .../healthz/ready
  4. Read a representative encrypted customer attribute back.
  5. Verify the audit chain:                    merlon-audit verify

Step 4 is the one that catches a key-ring mismatch. Do not skip it: a restore
that passes every other check can still have produced unreadable data.
EOF
