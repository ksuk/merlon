#!/usr/bin/env bash
set -euo pipefail

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
#   MERLON_DATABASE_URL=postgres://... \
#   MERLON_ENCRYPTION_KEY_RING=... \
#     scripts/backup.sh [output-directory]
#
# Environment:
#   MERLON_DATABASE_URL         required; the database to dump
#   MERLON_ENCRYPTION_KEY_RING  the key ring to capture (see --no-keys)
#
# Options:
#   --no-keys   acknowledge that no key ring is captured. Only correct when
#               the deployment genuinely has none, i.e. PII was never
#               encrypted. Never correct for a production database.

usage() {
  sed -n '3,25p' "$0" | sed 's/^# \{0,1\}//'
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

if [[ -z ${MERLON_DATABASE_URL:-} ]]; then
  echo "MERLON_DATABASE_URL is required" >&2
  exit 1
fi

if ! command -v pg_dump >/dev/null 2>&1; then
  echo "pg_dump not found. Install the PostgreSQL client tools, or run this" >&2
  echo "script inside a container that has them." >&2
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

db_file="$out_dir/merlon-db-$timestamp.dump"
keys_file="$out_dir/merlon-keyring-$timestamp.env"
manifest_file="$out_dir/merlon-backup-$timestamp.json"

echo "Dumping database to $db_file"
# Custom format: compressed, and restorable selectively with pg_restore.
pg_dump --format=custom --no-owner --no-privileges \
  --file="$db_file" "$MERLON_DATABASE_URL"

keys_sha=""
if [[ -n ${MERLON_ENCRYPTION_KEY_RING:-} ]]; then
  echo "Writing key ring to $keys_file"
  # Created empty with restrictive permissions before the secret is written,
  # so it is never briefly world-readable.
  install -m 600 /dev/null "$keys_file"
  printf 'MERLON_ENCRYPTION_KEY_RING=%s\n' "$MERLON_ENCRYPTION_KEY_RING" > "$keys_file"
  keys_sha=$(sha256sum "$keys_file" | cut -d ' ' -f 1)
fi

db_sha=$(sha256sum "$db_file" | cut -d ' ' -f 1)

cat > "$manifest_file" <<EOF
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
