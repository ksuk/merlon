#!/usr/bin/env bash
# Runs a non-interactive Codex review of the diff between the local `main`
# branch and the current working tree. Invoked by /codex-direct-review.
set -euo pipefail

if [ "$#" -ne 0 ]; then
  echo "Error: codex-direct-review.sh does not accept any arguments" >&2
  exit 1
fi

if ! command -v codex >/dev/null 2>&1; then
  echo "Error: codex CLI not found on PATH" >&2
  exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "Error: not inside a git repository" >&2
  exit 1
}
cd "$REPO_ROOT"

if ! git show-ref --verify --quiet refs/heads/main; then
  echo "Error: local branch 'main' not found in ${REPO_ROOT}" >&2
  exit 1
fi

# --dangerously-bypass-approvals-and-sandbox is a deliberate, user-specified
# choice: it disables Codex's own approval prompts and sandboxing while it
# reads and analyzes an actual repo diff, which may itself be adversarial
# (e.g. a malicious PR branch). Only invoke this against diffs you already
# trust enough to run `git diff`/`git log` on locally, and be aware that
# prompt injection in the reviewed diff could make Codex run host-level
# commands with full filesystem and network access. Codex's own sandbox
# (bwrap) does not work in this devcontainer (no unprivileged user
# namespaces), so real containment has to come from the caller, not from
# --sandbox; as a partial mitigation, the environment handed to Codex below
# is reduced to the minimum it needs, so a jailbroken Codex process cannot
# casually read unrelated secrets out of the ambient shell environment.
CODEX_ENV=(PATH="$PATH" HOME="$HOME")
if [ -n "${CODEX_HOME:-}" ]; then
  CODEX_ENV+=(CODEX_HOME="$CODEX_HOME")
fi

exec env -i "${CODEX_ENV[@]}" codex exec --ephemeral review \
  --base main \
  --dangerously-bypass-approvals-and-sandbox
