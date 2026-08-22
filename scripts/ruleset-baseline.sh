#!/usr/bin/env bash
# The single source of truth for what a committed ruleset baseline looks like.
#
# `.github/rulesets/` holds the GitHub rulesets that protect `main` and release
# tags, so that weakening one appears as a reviewable diff rather than only in
# a settings UI nobody re-reads. Three places used to hand-copy the same jq
# filter to produce those files -- the drift workflow, the README, and nothing
# at all in configure-github-ruleset.sh despite the docs claiming it exported.
# They are all callers of this script now, because a canonical form maintained
# in three copies is a canonical form waiting to disagree with itself.
#
# The reason this is bash+jq rather than Node, given that check-self-review.mjs
# set the precedent for a testable gate: `jq -S` reproduces both committed
# baselines byte for byte today. Reimplementing the canonicalization in another
# language would have to match jq's exact output or force every baseline to be
# regenerated in the same change -- risk bought for no benefit. What the .mjs
# precedent actually establishes is that the *judgment* belongs somewhere tests
# can reach, not that it belongs in JavaScript. So --canonicalize and --check
# are pure (stdin/files in, verdict out, no gh, no git, no network) and carry
# every decision; --export-all is the thin orchestration that cannot be tested
# offline and deliberately decides nothing.
#
# Usage:
#   ruleset-baseline.sh --canonicalize < raw-ruleset.json   # validate + emit canonical
#   ruleset-baseline.sh --check FILE...                     # audit committed baselines
#   ruleset-baseline.sh --export-all DIR                    # fetch live -> DIR (needs gh)
#
# --canonicalize and --export-all accept --comparable, which drops bypass_actors
# from both the requirement and the output. That is the form the weekly drift
# job compares, because GITHUB_TOKEN cannot see the field at all -- see the
# three states below.
#
# Exit codes are distinct so callers can choose an annotation without matching
# on message text, which would make the prose load-bearing:
#   0  ok
#   1  malformed input
#   2  usage error
#   3  permission-degraded response (the token cannot see admin-only fields)
#   4  the requesting identity can bypass the ruleset
#
# bypass_actors has three states, and collapsing them is the failure this script
# exists to prevent:
#
#   verified-empty     an administrator's export saw []
#   verified-nonempty  an administrator's export saw actors listed -- a finding
#   unverifiable       the caller could not see the field
#
# "Absent" and "empty" are different claims. A caller without ruleset write
# access gets the key omitted, which is indistinguishable from [] to anything
# comparing values -- so the omission is never silently treated as emptiness.
# Strict mode (the default) refuses to proceed on it. Comparable mode drops the
# field from both sides deliberately and the caller states that it did.
set -euo pipefail

readonly EXIT_MALFORMED=1
readonly EXIT_USAGE=2
readonly EXIT_DEGRADED=3
readonly EXIT_BYPASSABLE=4

# Every field that decides what a ruleset enforces. The check is that each one
# is *present*, not that it holds a particular value -- absence is the failure
# this list exists to catch, because an absent field is indistinguishable from
# a benign value to anything comparing values. If GitHub ever stops returning
# one of these, the comparison silently narrows to whatever is left, and a
# guard that quietly checks less is worse than no guard: it still reports
# success. Same rule as the other guards in this directory, stated in
# docs/security/supply-chain.md.
# Three lists, not one, because a raw API response and a committed baseline are
# not the same object and must not share a schema. Collapsing them is how a
# relaxation meant for one caller silently relaxes the audit of the other.
#
# What a raw response must carry to be trusted. `current_user_can_bypass` is
# here even though it is stripped from the committed form: it has to be
# *observed* to be asserted, and it is the only evidence that the identity
# running the export cannot sidestep the ruleset it is exporting.
readonly RESPONSE_KEYS=(
  bypass_actors conditions current_user_can_bypass enforcement name rules target
)

# What a raw response must carry when the caller cannot see administration
# fields. Both per-permission fields drop out together: a token that cannot read
# `bypass_actors` has no standing to answer the bypass question either, and
# accepting a missing `current_user_can_bypass` as "never" would be the same
# absent-reads-as-benign failure one field over. Comparable mode therefore
# asserts nothing about bypassing, and says so.
readonly RESPONSE_KEYS_COMPARABLE=(conditions enforcement name rules target)

# What a committed baseline must carry. `current_user_can_bypass` is absent by
# design (per-viewer, stripped at export), so requiring it here would fail every
# baseline that was correctly produced.
readonly BASELINE_KEYS=(bypass_actors conditions enforcement name rules target)

# Per-request or per-viewer values. They would produce a diff on every export
# depending on who ran it and when, so they are stripped from the committed
# form -- but `current_user_can_bypass` is asserted against *before* it is
# stripped (see canonicalize), which is why validation cannot be moved after
# the filter.
readonly FILTER='del(._links, .node_id, .created_at, .updated_at, .current_user_can_bypass)'

usage() {
  cat >&2 <<'USAGE'
usage:
  ruleset-baseline.sh --canonicalize [--comparable] < raw-ruleset.json
  ruleset-baseline.sh --check FILE...
  ruleset-baseline.sh --export-all [--comparable] DIR

--comparable drops bypass_actors from the requirement and the output, for
callers that cannot see it. Not valid with --check.
USAGE
}

# Validate one raw ruleset response and print its canonical form.
#
# Order matters and is not an implementation detail. Required keys are checked
# first so that a token which cannot see admin-only fields always dies with the
# token message; checking bypassability first would report the Actions app's
# viewpoint and send the reader after the wrong problem.
canonicalize() {
  local comparable=${1:-strict}
  local raw name
  local -a required=("${RESPONSE_KEYS[@]}")
  local filter="$FILTER"

  if [[ "$comparable" == comparable ]]; then
    required=("${RESPONSE_KEYS_COMPARABLE[@]}")
    # Dropped from the output as well as the requirement. Leaving it in would
    # put a field in the comparable form that one side can never populate,
    # which is the diff this mode exists to stop producing.
    filter="${FILTER%)} , .bypass_actors)"
  fi

  if ! raw=$(jq -e '.' 2>/dev/null); then
    echo "Input is not valid JSON." >&2
    return "$EXIT_MALFORMED"
  fi
  if [[ "$(jq -r 'type' <<<"$raw")" != "object" ]]; then
    echo "Expected a single ruleset object." >&2
    return "$EXIT_MALFORMED"
  fi

  name=$(jq -r '.name // "(unnamed)"' <<<"$raw")

  local key
  for key in "${required[@]}"; do
    if [[ "$(jq -r --arg k "$key" 'has($k)' <<<"$raw")" != "true" ]]; then
      degraded_error "$name" "$key"
      return "$EXIT_DEGRADED"
    fi
  done

  # GitHub's own answer to "can the caller sidestep this ruleset", which is
  # strictly better evidence than inferring it from an empty bypass_actors --
  # the two are not the same claim. It is per-viewer, so it is asserted here
  # and then deleted rather than committed.
  #
  # In comparable mode the field is not required and is not asserted: a caller
  # that cannot see administration fields cannot answer this question either,
  # and there is no default that would be honest. Absence is never read as
  # "never" -- that would be the whole absent-versus-empty failure again, one
  # field over. The strict path requires the key, so `// "never"` here can only
  # be reached in comparable mode, where the loop below is skipped entirely.
  local can_bypass
  can_bypass=$(jq -r '.current_user_can_bypass // "unverifiable"' <<<"$raw")
  if [[ "$can_bypass" != never && "$can_bypass" != unverifiable ]]; then
    cat >&2 <<EOF
Ruleset "$name" reports current_user_can_bypass = "$can_bypass" for the
identity this export runs as. The governance documentation asserts "never":
the maintainer is subject to the same required checks as everyone else. That
is no longer true.

This is a finding, not a token problem.
EOF
    return "$EXIT_BYPASSABLE"
  fi

  jq -S "$filter" <<<"$raw"
}

# The message the maintainer reads at 00:17 on a Monday. It has one job beyond
# explaining the cause: stop them from making the red go away by committing
# what was just exported. That is the action that converts a noisy failure into
# a permanently blind check, and it is the obvious action, so it is named and
# refused explicitly rather than left to be inferred.
degraded_error() {
  local name=$1 key=$2
  cat >&2 <<EOF
The exported ruleset "$name" has no "$key" key.

This is a TOKEN problem, not drift. The rulesets API returns administration
fields such as bypass_actors only to callers with *write* access to the
ruleset -- it will not show you who can bypass a rule unless you could also
change that rule. A caller without it receives the key omitted entirely, not an
empty list, and an omitted key is indistinguishable from "no bypass actors" to
anything that compares values.

Do NOT commit this export, and do NOT remove the key from
.github/rulesets/*.json to make this pass. A baseline without it would report
green forever while being structurally unable to see a bypass actor being
added, which is the single weakening this baseline exists to catch.

Reading it requires Administration write, which is a credential that can
delete every ruleset here. This project does not store one in Actions. Run
this export locally with an administrator's own token instead:

  REPO=<owner>/<name> bash scripts/ruleset-baseline.sh --export-all .github/rulesets
EOF
}

# Audit committed baselines offline. This is the half that runs on every pull
# request, and it is the half that matters most: the drift workflow is weekly
# and never sees a pull request, so a degraded baseline committed by hand would
# otherwise sit there until the next Monday -- or forever, if the export that
# produced it also became the thing it was compared against.
check_files() {
  local -a files=("$@")
  local failures=0

  # Finding zero files is a finding. A glob that matches nothing would
  # otherwise make this pass by checking nothing at all.
  if [[ ${#files[@]} -eq 0 ]]; then
    echo "No ruleset baseline files given. Expected at least one." >&2
    return "$EXIT_MALFORMED"
  fi

  local file key
  for file in "${files[@]}"; do
    if [[ ! -f "$file" ]]; then
      echo "$file: not a file" >&2
      failures=1
      continue
    fi
    if ! jq -e '.' "$file" >/dev/null 2>&1; then
      echo "$file: not valid JSON" >&2
      failures=1
      continue
    fi

    for key in "${BASELINE_KEYS[@]}"; do
      if [[ "$(jq -r --arg k "$key" 'has($k)' "$file")" != "true" ]]; then
        echo "$file: missing required key \"$key\"." >&2
        echo "  A baseline without it cannot evidence what the drift check compares." >&2
        failures=1
      fi
    done

    local stripped
    for stripped in _links node_id created_at updated_at current_user_can_bypass; do
      if [[ "$(jq -r --arg k "$stripped" 'has($k)' "$file")" == "true" ]]; then
        echo "$file: contains \"$stripped\", which the canonical form strips." >&2
        failures=1
      fi
    done

    # Idempotence. Running the canonical filter over a canonical file has to
    # reproduce it exactly; if it does not, the file was hand-edited or was
    # produced by a different filter, and either way it is no longer the thing
    # the drift check will compare against.
    if ! diff -q <(jq -S "$FILTER" "$file") "$file" >/dev/null 2>&1; then
      echo "$file: not in canonical form." >&2
      echo "  Re-export it rather than editing it by hand:" >&2
      echo "  bash scripts/ruleset-baseline.sh --export-all .github/rulesets" >&2
      failures=1
    fi
  done

  if [[ $failures -ne 0 ]]; then
    return "$EXIT_MALFORMED"
  fi
  echo "Ruleset baselines carry every field the drift check compares (${#files[@]} file(s))."
}

# Fetch every live ruleset into DIR, replacing whatever was there. The only mode
# that needs the network, and deliberately the only mode with no judgment in it
# -- every verdict below is canonicalize's.
#
# The replacement is staged, not transactional: everything is built and
# validated in a scratch directory, then copied under temporary names inside
# DIR before any existing baseline is touched. Writing straight into DIR made
# the release-checklist command fail on a normal checkout because the files it
# was about to produce were already there and the collision guard reported that
# two live rulesets shared a name. Clearing DIR before the fetch had the opposite
# failure: a mid-way network or validation error left the baseline short a file.
# Staging closes both of those windows and ensures a copy failure leaves the
# existing JSON intact.
#
# POSIX has no atomic directory replacement. The final commit therefore renames
# each staged file into place and removes obsolete JSON only after every new file
# is ready. Each rename is atomic because the temporary directory is created
# inside DIR, on the same filesystem, but the set of renames is not: a signal or
# mv failure can leave a mixture of the previous and new exports (and obsolete
# files may remain until the removal loop finishes). Restore tracked files with
# `git checkout -- .github/rulesets/`; if a newly added ruleset left an untracked
# JSON file, identify it with `git status --short .github/rulesets/` and remove
# that file explicitly. Then investigate the failure and rerun the export.
export_all() {
  local dir=$1 comparable=${2:-strict} repo=${REPO:-${GITHUB_REPOSITORY:-}}
  local scratch commit_stage

  if [[ -z "$repo" ]]; then
    echo "Set REPO or GITHUB_REPOSITORY to owner/name." >&2
    return "$EXIT_USAGE"
  fi
  if [[ ! -d "$dir" ]]; then
    echo "Not a directory: $dir" >&2
    return "$EXIT_USAGE"
  fi
  command -v gh >/dev/null 2>&1 || { echo "gh is required for --export-all" >&2; return "$EXIT_USAGE"; }

  # --paginate because a repository with more rulesets than fit one page would
  # otherwise have the overflow silently drop out of the comparison. The list
  # is read into a variable first so that a failed call aborts here, under
  # `set -e`, rather than producing an empty loop that looks like success.
  local ids
  ids=$(gh api --paginate "repos/$repo/rulesets" --jq '.[].id')

  scratch=$(mktemp -d)
  # shellcheck disable=SC2064  # expand $scratch now, not at trap time
  trap "rm -rf '$scratch'" RETURN

  if [[ -z "$ids" ]]; then
    echo "No rulesets exist on $repo." >&2
    echo "This is not an empty comparison to pass over: it means the branch and" >&2
    echo "tag protections are off right now." >&2
    return "$EXIT_MALFORMED"
  fi

  local id raw name status
  for id in $ids; do
    # One fetch per ruleset. Fetching once for the name and again for the body
    # would leave a window where the two disagree, besides doubling the calls.
    raw=$(gh api "repos/$repo/rulesets/$id")
    name=$(jq -r '.name // ""' <<<"$raw")

    # The output path is derived from remote data, so it is constrained before
    # it is used rather than trusted.
    if [[ ! "$name" =~ ^[A-Za-z0-9._-]+$ ]]; then
      echo "Ruleset $id has a name unusable as a filename: ${name@Q}" >&2
      return "$EXIT_MALFORMED"
    fi
    # The scratch directory starts empty, so this can only fire on a genuine
    # collision between two live rulesets -- never on a file that was simply
    # already committed.
    if [[ -e "$scratch/$name.json" ]]; then
      echo "Two live rulesets share the name \"$name\"; refusing to overwrite." >&2
      return "$EXIT_MALFORMED"
    fi

    status=0
    canonicalize "$comparable" <<<"$raw" > "$scratch/$name.json" || status=$?
    if [[ $status -ne 0 ]]; then
      return "$status"
    fi
  done

  # Copy under temporary names on the target filesystem before entering the
  # per-file commit window. If this copy fails, RETURN cleanup removes only the
  # hidden staging directory and every existing baseline remains byte-for-byte
  # intact.
  commit_stage=$(mktemp -d "$dir/.ruleset-export.XXXXXX")
  # shellcheck disable=SC2064  # expand both validated mktemp paths now
  trap "rm -rf '$scratch' '$commit_stage'" RETURN
  status=0
  cp "$scratch"/*.json "$commit_stage"/ || status=$?
  if [[ $status -ne 0 ]]; then
    # An explicit return is needed for the RETURN trap. Letting `set -e` abort
    # the shell here would preserve the baselines but strand the hidden stage.
    return "$status"
  fi

  # Commit one file at a time. A rename within DIR replaces an existing file
  # atomically, so the old file is never removed before its complete successor
  # is ready. The loop as a whole cannot be atomic; the function comment above
  # states the residual mixed-version window and recovery instead of calling it
  # a transaction that POSIX cannot provide.
  local staged target
  for staged in "$commit_stage"/*.json; do
    name=${staged##*/}
    status=0
    mv -f -- "$staged" "$dir/$name" || status=$?
    if [[ $status -ne 0 ]]; then
      return "$status"
    fi
  done

  # Remove rulesets that no longer exist only after all replacements landed.
  # Anything else in DIR (README.md and the hidden stage) is not export output.
  # An interruption here leaves obsolete evidence visible rather than deleting
  # a baseline that the newly staged export expected to contain.
  for target in "$dir"/*.json; do
    [[ -e "$target" ]] || continue
    name=${target##*/}
    if [[ ! -e "$scratch/$name" ]]; then
      status=0
      rm -f -- "$target" || status=$?
      if [[ $status -ne 0 ]]; then
        return "$status"
      fi
    fi
  done
}

main() {
  # --comparable is accepted in any position so that neither `--export-all DIR
  # --comparable` nor `--comparable --export-all DIR` is a surprise. It is never
  # valid with --check: a committed baseline is written by an administrator and
  # must carry bypass_actors, and letting the audit relax that would let a
  # degraded baseline pass the very check that exists to catch it.
  local mode=strict
  local -a args=()
  local a
  for a in "$@"; do
    if [[ "$a" == --comparable ]]; then
      mode=comparable
    else
      args+=("$a")
    fi
  done

  case "${args[0]:-}" in
    --canonicalize)
      [[ ${#args[@]} -eq 1 ]] || { usage; return "$EXIT_USAGE"; }
      canonicalize "$mode"
      ;;
    --check)
      if [[ "$mode" == comparable ]]; then
        echo "--check does not accept --comparable: a committed baseline must" >&2
        echo "carry bypass_actors, and relaxing that here would let a degraded" >&2
        echo "baseline pass the check that exists to catch it." >&2
        return "$EXIT_USAGE"
      fi
      check_files "${args[@]:1}"
      ;;
    --export-all)
      [[ ${#args[@]} -eq 2 ]] || { usage; return "$EXIT_USAGE"; }
      export_all "${args[1]}" "$mode"
      ;;
    *)
      usage
      return "$EXIT_USAGE"
      ;;
  esac
}

main "$@"
