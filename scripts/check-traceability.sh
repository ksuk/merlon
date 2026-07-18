#!/usr/bin/env bash
set -euo pipefail

body_only=false
if [[ "${1:-}" == "--body-only" ]]; then
  body_only=true
elif [[ $# -ne 0 ]]; then
  echo "usage: $0 [--body-only]" >&2
  exit 2
fi

is_bot() {
  local identity="${1,,}"
  [[ "$identity" == *"[bot]"* || "$identity" == *"[bot]@users.noreply.github.com"* ]]
}

field_value() {
  local label=$1
  awk -v label="$label" '
    index($0, label) == 1 {
      value = substr($0, length(label) + 1)
      sub(/^[[:space:]]+/, "", value)
      if (value != "") {
        print value
        exit
      }
      waiting = 1
      next
    }
    waiting && $0 !~ /^[[:space:]]*$/ {
      print
      exit
    }
  ' <<<"${PR_BODY:-}"
}

actor=${ACTOR:-}
if [[ -n "$actor" ]] && is_bot "$actor"; then
  echo "Traceability check skipped for bot actor: $actor"
  exit 0
fi

requirement=$(field_value "Requirement / issue:")
if ! grep -Eq '(^|[[:space:](])#[1-9][0-9]*($|[[:space:]),.;])' <<<"$requirement" &&
   ! grep -Eq 'https://github\.com/[^/[:space:]]+/[^/[:space:]]+/issues/[1-9][0-9]*([/?#][^[:space:]]*)?' <<<"$requirement"; then
  echo "PR body must provide Requirement / issue as #<number> or a public GitHub issue URL." >&2
  exit 1
fi

design=$(field_value "Public ADR or design reference:")
if [[ -z "$design" ]]; then
  echo "PR body must provide a public ADR or design reference." >&2
  exit 1
fi
if [[ "$design" =~ ^(N/A|n/a)[[:space:]]*$ ]]; then
  echo "N/A design references must include a short rationale, for example: N/A — documentation-only." >&2
  exit 1
fi

if $body_only; then
  echo "PR traceability fields are valid."
  exit 0
fi

base_sha=${BASE_SHA:-}
if [[ -z "$base_sha" ]]; then
  echo "BASE_SHA is required unless --body-only is used." >&2
  exit 2
fi
if ! git cat-file -e "${base_sha}^{commit}" 2>/dev/null; then
  echo "BASE_SHA does not identify a fetched commit: $base_sha" >&2
  exit 2
fi

failures=0
while IFS= read -r commit; do
  author_name=$(git show -s --format=%an "$commit")
  author_email=$(git show -s --format=%ae "$commit")
  if is_bot "$author_name" || is_bot "$author_email"; then
    continue
  fi

  message=$(git show -s --format=%B "$commit")
  if ! grep -Eq '^Refs #[1-9][0-9]*([, ]+#[1-9][0-9]*)*$' <<<"$message"; then
    echo "Commit $commit ($author_name) is missing a 'Refs #<number>' footer." >&2
    failures=1
  fi
done < <(git rev-list --no-merges "${base_sha}..HEAD")

if [[ $failures -ne 0 ]]; then
  exit 1
fi

echo "PR and commit traceability are valid."
