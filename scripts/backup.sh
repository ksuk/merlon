#!/usr/bin/env bash
set -euo pipefail
umask 077

# Take a Merlon backup: a PostgreSQL dump plus the encryption key ring.
#
# The two are written as SEPARATE files, deliberately. A Merlon backup is not
# one artifact — encrypted customer attributes are unreadable without the key
# ring, and a key ring stored next to the dump it decrypts protects nothing.
# The single most common way to lose this data permanently is to back up the
# database faithfully for months and never once capture the keys, so this
# script refuses to produce a database-only backup silently.
#
# Usage:
#   MERLON_BACKUP_DATABASE_URL=postgres://... \
#   MERLON_ENCRYPTION_KEY_RING=... \
#     scripts/backup.sh [output-directory]
#
# Environment:
#   MERLON_BACKUP_DATABASE_URL  dedicated read-only backup connection, as a URL
#   MERLON_ENCRYPTION_KEY_RING  the key ring to capture (see --no-keys)
#
# The connection may instead be supplied through libpq's own variables
# (PGHOST/PGPORT/PGDATABASE/PGUSER/PGPASSWORD, PGSERVICE, or ~/.pgpass), which
# is what production deployments should do: a URL is passed to pg_dump as a
# command-line argument, where `ps` and /proc/<pid>/cmdline expose its password
# to every co-resident process for as long as the dump runs.
#
# Options:
#   --no-keys   acknowledge that no key ring is captured. Only correct when
#               the deployment genuinely has none, i.e. PII was never
#               encrypted. Never correct for a production database.

usage() {
  awk '
    /^# / {
      printing = 1
      sub(/^# ?/, "")
      print
      next
    }
    /^#$/ {
      if (printing) print ""
      next
    }
    printing { exit }
  ' "$0"
}

no_keys=false
out_dir=

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-keys) no_keys=true; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    *)
      if [[ -n $out_dir ]]; then
        echo "unexpected argument: $1" >&2
        exit 2
      fi
      out_dir=$1
      shift
      ;;
  esac
done

out_dir=${out_dir:-backups}

# Either form of connection is passed to every client the same way, as an
# argument array. When libpq's own variables are in use the array is empty and
# libpq reads them itself, rather than this script reassembling a DSN it would
# then have to put back on the command line -- which is the exposure being
# avoided.
conn_args=()
if [[ -n ${MERLON_BACKUP_DATABASE_URL:-} ]]; then
  conn_args=(--dbname "$MERLON_BACKUP_DATABASE_URL")
elif [[ -z ${PGDATABASE:-}${PGSERVICE:-}${PGHOST:-} ]]; then
  cat >&2 <<'EOF'
MERLON_BACKUP_DATABASE_URL is required, or a libpq connection environment.

Set MERLON_BACKUP_DATABASE_URL to a dedicated read-only connection, or set
PGDATABASE (with PGHOST/PGPORT/PGUSER/PGPASSWORD as needed) or PGSERVICE. The
libpq variables keep the password out of the process table and are the better
choice for a scheduled production backup.
EOF
  exit 1
fi

if ! command -v pg_dump >/dev/null 2>&1; then
  echo "pg_dump not found. Install the PostgreSQL client tools, or run this" >&2
  echo "script inside a container that has them." >&2
  exit 1
fi
if ! command -v psql >/dev/null 2>&1; then
  echo "psql not found. Install the PostgreSQL client tools, or run this" >&2
  echo "script inside a container that has them." >&2
  exit 1
fi

# Merlon does not support PostgreSQL large objects in this logical-backup
# contract. Reject that object model explicitly before pg_dump can fail late
# with a generic permission error or produce artifacts outside the contract.
large_object_count=$(psql "${conn_args[@]}" \
  --no-psqlrc --tuples-only --no-align --set ON_ERROR_STOP=1 \
  --command "SELECT count(*) FROM pg_catalog.pg_largeobject_metadata")
if [[ ! $large_object_count =~ ^[0-9]+$ ]]; then
  echo "could not verify the PostgreSQL large-object precondition" >&2
  exit 1
fi
if (( large_object_count != 0 )); then
  echo "PostgreSQL large objects are not supported by the Merlon logical backup; refusing to create an incomplete backup" >&2
  exit 1
fi

# The key ring decision is made before anything is written, so the run either
# produces a restorable backup or stops.
if [[ -z ${MERLON_ENCRYPTION_KEY_RING:-} ]]; then
  if [[ $no_keys != true ]]; then
    cat >&2 <<'EOF'
MERLON_ENCRYPTION_KEY_RING is not set.

If this deployment encrypts customer PII, a database-only backup is
unrecoverable: there is no way to read those attributes back without the key
ring, and no recovery path exists after the fact.

Set MERLON_ENCRYPTION_KEY_RING, or pass --no-keys if this deployment genuinely
stores no encrypted attributes.
EOF
    exit 1
  fi
  echo "warning: proceeding without a key ring (--no-keys)" >&2
fi

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$out_dir"
chmod 700 "$out_dir"

db_file="$out_dir/merlon-db-$timestamp.dump"
keys_file="$out_dir/merlon-keyring-$timestamp.env"
manifest_file="$out_dir/merlon-backup-$timestamp.json"

db_temp=$(mktemp "$out_dir/.merlon-db-$timestamp.dump.XXXXXX")
keys_temp=
manifest_temp=$(mktemp "$out_dir/.merlon-backup-$timestamp.json.XXXXXX")

cleanup() {
  [[ -z $db_temp ]] || rm -f -- "$db_temp"
  [[ -z $keys_temp ]] || rm -f -- "$keys_temp"
  [[ -z $manifest_temp ]] || rm -f -- "$manifest_temp"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Dumping database to $db_file"
# Custom format: compressed, and restorable selectively with pg_restore.
pg_dump --format=custom --no-owner --no-privileges \
  --file="$db_temp" "${conn_args[@]}"

keys_sha=""
if [[ -n ${MERLON_ENCRYPTION_KEY_RING:-} ]]; then
  echo "Writing key ring to $keys_file"
  keys_temp=$(mktemp "$out_dir/.merlon-keyring-$timestamp.env.XXXXXX")
  chmod 600 "$keys_temp"
  printf 'MERLON_ENCRYPTION_KEY_RING=%s\n' "$MERLON_ENCRYPTION_KEY_RING" > "$keys_temp"
  keys_sha=$(sha256sum "$keys_temp" | cut -d ' ' -f 1)
fi

db_sha=$(sha256sum "$db_temp" | cut -d ' ' -f 1)

cat > "$manifest_temp" <<EOF
{
  "schema_version": 1,
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "database": {
    "file": "$(basename "$db_file")",
    "sha256": "$db_sha",
    "format": "pg_dump custom"
  },
  "key_ring": $(
    if [[ -n $keys_sha ]]; then
      printf '{"file": "%s", "sha256": "%s"}' "$(basename "$keys_file")" "$keys_sha"
    else
      printf 'null'
    fi
  )
}
EOF

# Publish the manifest last. Until it appears, no set of final-name artifacts
# is advertised as a complete backup; failures remove every temporary file.
mv -- "$db_temp" "$db_file"
db_temp=
if [[ -n $keys_temp ]]; then
  mv -- "$keys_temp" "$keys_file"
  keys_temp=
fi
mv -- "$manifest_temp" "$manifest_file"
manifest_temp=

echo
echo "Backup complete:"
echo "  database:  $db_file"
if [[ -n $keys_sha ]]; then
  echo "  key ring:  $keys_file"
fi
echo "  manifest:  $manifest_file"
echo

if [[ -n $keys_sha ]]; then
  cat <<'EOF'
Store the key ring somewhere the database backup is NOT.

Anyone holding both files holds the plaintext customer data. Anyone holding
neither cannot restore. Retain retired keys for at least as long as you retain
backups written under them, or those backups become unreadable at the next
rotation.
EOF
fi
