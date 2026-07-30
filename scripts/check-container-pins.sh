#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

files=(
  docker-compose.yml
  docker-compose.demo.yml
  .github/workflows/ci.yml
)

expected=
for file in "${files[@]}"; do
  mapfile -t refs < <(
    sed -nE 's/^[[:space:]]*image:[[:space:]]*"?([^"[:space:]]+)"?.*$/\1/p' "$file" |
      awk '/^postgres:/'
  )

  if [[ ${#refs[@]} -ne 1 ]]; then
    echo "$file must contain exactly one literal postgres image reference" >&2
    exit 1
  fi

  ref=${refs[0]}
  if [[ ! $ref =~ ^postgres:[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]]; then
    echo "$file must pin postgres by tag and sha256 digest (got: $ref)" >&2
    exit 1
  fi

  if [[ -z $expected ]]; then
    expected=$ref
  elif [[ $ref != "$expected" ]]; then
    echo "postgres image pins are inconsistent" >&2
    echo "expected: $expected" >&2
    echo "$file: $ref" >&2
    exit 1
  fi
done

echo "postgres image pins are synchronized: $expected"
