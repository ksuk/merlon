#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

# Configuration is entirely environment-driven, and the same variable is
# therefore declared in up to three places: the Go code that reads it,
# docs/configuration.md which is the reference operators are pointed at, and
# .env.example which is what they copy. Nothing forces those to agree, so they
# drift in both directions -- a variable the code reads that no operator can
# discover, and a variable operators are told to set that nothing reads. Both
# are silent: the first looks like a missing feature, the second looks like it
# works.
#
# This guard is the same shape as check-container-pins.sh and
# check-toolchain-pins.sh: a value declared twice must match, and finding zero
# of something is an error rather than a pass.

config_doc=docs/configuration.md
env_example=.env.example

# Variables that are legitimately not read by the Go code. Each needs a reason,
# because "add it to the allowlist" is otherwise the path of least resistance
# for exactly the drift this guard exists to catch.
#
#   MERLON_POSTGRES_PASSWORD -- consumed by Compose to build the connection
#   string and to initialize the postgres container. The application only ever
#   sees the assembled MERLON_DATABASE_URL.
#   MERLON_BACKUP_DATABASE_URL -- consumed by scripts/backup.sh, not by the Go
#   serving or migration processes. It must identify a dedicated read-only
#   backup role.
#   MERLON_API_HOST_PORT -- consumed by Docker Compose for the host-side API
#   binding; the Go process continues to listen on its container port.
#   MERLON_DB_HOST_PORT -- consumed by the test-only Docker Compose overlay for
#   its loopback PostgreSQL binding; the standard/demo topologies do not expose
#   PostgreSQL to the host.
allowlist=(
  MERLON_BACKUP_DATABASE_URL
  MERLON_POSTGRES_PASSWORD
)

# The Compose-only variables are optional to this guard's minimal fixture in
# scripts/test_check_env_vars.py. Enable each exception only after both
# operator-facing sources declare it; if either source is missing, the normal
# drift checks below must report that inconsistency.
if grep -qE '`MERLON_API_HOST_PORT`' "$config_doc" &&
  grep -qE '^[[:space:]]*#?[[:space:]]*MERLON_API_HOST_PORT[[:space:]]*=' "$env_example"; then
  allowlist+=(MERLON_API_HOST_PORT)
fi
if grep -qE '`MERLON_DB_HOST_PORT`' "$config_doc" &&
  grep -qE '^[[:space:]]*#?[[:space:]]*MERLON_DB_HOST_PORT[[:space:]]*=' "$env_example"; then
  allowlist+=(MERLON_DB_HOST_PORT)
fi

# code_vars -- every MERLON_* name the Go code actually reads. Restricted to
# the call forms that read an environment variable, so a name appearing only in
# a comment or an error string is not mistaken for a read.
code_vars() {
  grep -rhoE '(os\.Getenv|os\.LookupEnv|getEnv[A-Za-z]*|getenv|envOr)\("MERLON_[A-Z0-9_]+"' \
    api/ --include='*.go' |
    grep -oE 'MERLON_[A-Z0-9_]+' | sort -u
}

# doc_vars -- every MERLON_* name documented in the configuration reference.
doc_vars() {
  grep -oE '`MERLON_[A-Z0-9_]+`' "$config_doc" | tr -d '`' | sort -u
}

# example_vars -- every MERLON_* name in .env.example, including commented-out
# entries, since a commented example is still something an operator will copy.
example_vars() {
  grep -oE '^[[:space:]]*#?[[:space:]]*MERLON_[A-Z0-9_]+[[:space:]]*=' "$env_example" |
    grep -oE 'MERLON_[A-Z0-9_]+' | sort -u
}

allowed() {
  printf '%s\n' "${allowlist[@]}" | sort -u
}

mapfile -t code < <(code_vars)
mapfile -t documented < <(doc_vars)
mapfile -t example < <(example_vars)

# Anti-vacuous: if an extraction stops matching -- a refactor renames the
# helper, the doc changes format -- every comparison below would trivially
# pass while checking nothing.
for name in code documented example; do
  declare -n arr="$name"
  if [[ ${#arr[@]} -eq 0 ]]; then
    echo "extracted no variables for '$name'; this guard cannot compare anything" >&2
    exit 1
  fi
done

status=0

# 1. Everything the code reads must be documented. An undocumented variable is
#    one an operator cannot find, configure, or review.
undocumented=$(comm -23 <(code_vars) <(doc_vars))
if [[ -n $undocumented ]]; then
  echo "read by the code but absent from $config_doc:" >&2
  printf '  %s\n' $undocumented >&2
  status=1
fi

# 2. Nothing may be documented that the code does not read, unless allowlisted.
#    A documented variable that nothing reads is worse than an omission: it
#    reads as configured when it has no effect.
unread=$(comm -13 <(code_vars) <(doc_vars) | comm -23 - <(allowed))
if [[ -n $unread ]]; then
  echo "documented in $config_doc but read nowhere in api/:" >&2
  printf '  %s\n' $unread >&2
  echo "Remove it, or add it to the allowlist in $0 with a reason." >&2
  status=1
fi

# 3. Same rule for .env.example, which operators copy verbatim.
dead=$(comm -13 <(code_vars) <(example_vars) | comm -23 - <(allowed))
if [[ -n $dead ]]; then
  echo "present in $env_example but read nowhere in api/:" >&2
  printf '  %s\n' $dead >&2
  status=1
fi

# 4. An allowlist entry for a variable that is now read by the code, or has
#    disappeared entirely, is stale and must not keep silently excusing it.
for name in $(allowed); do
  if code_vars | grep -qx "$name"; then
    echo "$name is allowlisted but is now read by the code; remove the entry" >&2
    status=1
  elif ! doc_vars | grep -qx "$name" && ! example_vars | grep -qx "$name"; then
    echo "$name is allowlisted but appears nowhere; remove the entry" >&2
    status=1
  fi
done

if [[ $status -ne 0 ]]; then
  exit 1
fi

echo "environment variables are synchronized: ${#code[@]} read, ${#documented[@]} documented, ${#example[@]} in $env_example"
