#!/usr/bin/env bash
set -euo pipefail

DB_URL="${MERLON_DATABASE_URL:-postgres://merlon:merlon@localhost:5432/merlon?sslmode=disable}"
SEED_FILE="$(dirname "$0")/../deploy/seed/legacy/seed.sql"

if ! command -v psql &> /dev/null; then
  echo "Error: psql is not installed."
  exit 1
fi

echo "Warning: deploy/seed/legacy/seed.sql predates the current DB schema" \
  "(see deploy/seed/legacy/README.md) and is expected to fail against it." >&2

echo "Seeding demo data..."
psql "$DB_URL" -f "$SEED_FILE"
echo "Demo data seeded successfully."
