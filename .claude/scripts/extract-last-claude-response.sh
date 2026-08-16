#!/usr/bin/env bash
# Extracts the last assistant text response from a Claude Code session
# transcript (JSONL) and prints it to stdout. Used by codex-review-last.sh.
set -euo pipefail

SCRIPT_NAME="$(basename "$0")"

if [ "$#" -ne 1 ]; then
  echo "Usage: ${SCRIPT_NAME} <SESSION_ID>" >&2
  exit 1
fi

SESSION_ID="$1"

# Strict UUID validation: the session id is used to build a filesystem path,
# so it must not contain path separators or traversal sequences.
if ! [[ "$SESSION_ID" =~ ^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$ ]]; then
  echo "Error: invalid session id '${SESSION_ID}' (expected UUID format)" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "Error: jq not found on PATH" >&2
  exit 1
fi

CONFIG_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
PROJECTS_DIR="${CONFIG_DIR}/projects"

if [ ! -d "$PROJECTS_DIR" ]; then
  echo "Error: projects directory not found: ${PROJECTS_DIR}" >&2
  exit 1
fi

MATCHES=()
while IFS= read -r -d '' match; do
  MATCHES+=("$match")
done < <(find "$PROJECTS_DIR" -type f -name "${SESSION_ID}.jsonl" -print0)

if [ "${#MATCHES[@]}" -eq 0 ]; then
  echo "Error: no transcript found for session ${SESSION_ID}" >&2
  exit 1
fi

if [ "${#MATCHES[@]}" -gt 1 ]; then
  echo "Error: multiple transcripts found for session ${SESSION_ID}:" >&2
  printf '  %s\n' "${MATCHES[@]}" >&2
  exit 1
fi

TRANSCRIPT="${MATCHES[0]}"

# jq reads the file as a stream of concatenated JSON values (one per line).
# If the file's tail is still being written, the final value may be
# truncated; jq emits a parse error for it but still flushes every complete
# value it already parsed. That partial-tail error is expected and ignored
# here -- only fully-written entries are ever used.
RESPONSES="$(jq -c '
  select(.type == "assistant"
    and .message.role == "assistant"
    and ((.isSidechain // false) == false))
  | ([ .message.content[]? | select(.type == "text") | .text ] | join(""))
' "$TRANSCRIPT" 2>/dev/null || true)"

LAST_RESPONSE=""
while IFS= read -r line; do
  [ -z "$line" ] && continue
  [ "$line" = '""' ] && continue
  LAST_RESPONSE="$line"
done <<<"$RESPONSES"

if [ -z "$LAST_RESPONSE" ]; then
  echo "Error: no assistant text response found in session ${SESSION_ID}" >&2
  exit 1
fi

jq -r '.' <<<"$LAST_RESPONSE"
