#!/usr/bin/env bash
set -euo pipefail

# Phase 1 (default) is the configuration this repository operates today: one
# maintainer, no approving review, and `Governance Required` standing in for it
# by demanding a self-review record (ADR-0016). Phase 2 (--require-approvals)
# is the configuration to switch to when a second Admin exists and is listed in
# .github/CODEOWNERS — it requires an approving review from someone other than
# the author, so applying it before then would make every pull request
# unmergeable, including the ones adding the second maintainer.
#
# After changing the payload, apply it and refresh the committed baseline in
# .github/rulesets/ in the same pull request; the Ruleset Drift workflow
# compares the two.
apply=false
require_approvals=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply) apply=true ;;
    --require-approvals) require_approvals=true ;;
    *) echo "usage: $0 [--apply] [--require-approvals]" >&2; exit 2 ;;
  esac
  shift
done

command -v gh >/dev/null || { echo "gh is required" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

repository=${GITHUB_REPOSITORY:-}
if [[ -z "$repository" ]]; then
  repository=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
fi

# GitHub matches a required status check by check-run name, which is the job's
# `name:` -- not "<workflow> / <job>". The three gating workflows therefore give
# their aggregate jobs distinct names (CI Required, Security Required,
# Traceability Required); DCO's job declares no name, so its check-run is named
# after the job id. Any context listed here that never reports leaves every pull
# request pending forever, so only add checks that run unconditionally on every
# pull request targeting main. Build & Check Docs Site is deliberately absent:
# .github/workflows/docs-deploy.yml is path-filtered and does not report at all
# on pull requests that touch no documentation path.
#
# "Governance Required" is a commit status posted by .github/workflows/
# governance.yml, not a check-run: the self-review record it verifies arrives
# as a comment, after the push that produced the head commit. Add it only after
# that workflow is on main and has been seen reporting on a pull request.
required_contexts=$(jq -n '[
  "CI Required",
  "Security Required",
  "Traceability Required",
  "Governance Required",
  "check-signoffs"
] | map({context: .})')

pull_request_parameters=$(jq -n --argjson approvals "$require_approvals" '{
  allowed_merge_methods: ["squash"],
  dismiss_stale_reviews_on_push: true,
  require_code_owner_review: $approvals,
  require_last_push_approval: $approvals,
  required_approving_review_count: (if $approvals then 1 else 0 end),
  required_review_thread_resolution: true
}')

branch_payload=$(jq -n \
  --argjson pull_request "$pull_request_parameters" \
  --argjson contexts "$required_contexts" '{
  name: "main-release-governance",
  target: "branch",
  enforcement: "active",
  conditions: {ref_name: {include: ["refs/heads/main"], exclude: []}},
  rules: [
    {type: "deletion"},
    {type: "non_fast_forward"},
    {type: "required_linear_history"},
    {type: "pull_request", parameters: $pull_request},
    {type: "required_status_checks", parameters: {
      strict_required_status_checks_policy: true,
      do_not_enforce_on_create: false,
      required_status_checks: $contexts
    }}
  ],
  bypass_actors: []
}')

tag_payload=$(jq -n '{
  name: "release-tag-governance",
  target: "tag",
  enforcement: "active",
  conditions: {ref_name: {include: ["refs/tags/v*.*.*"], exclude: []}},
  rules: [
    {type: "deletion"},
    {type: "non_fast_forward"}
  ],
  bypass_actors: []
}')

if ! $apply; then
  if $require_approvals; then
    echo "Dry run (phase 2, approving review required):"
  else
    echo "Dry run (phase 1, single maintainer):"
  fi
  echo "the following active rulesets would be configured for $repository:"
  jq . <<<"$branch_payload"
  jq . <<<"$tag_payload"
  echo "Review the payload, re-run with --apply, then refresh .github/rulesets/."
  exit 0
fi

configure_ruleset() {
  local name=$1
  local payload=$2
  local id
  id=$(gh api "repos/${repository}/rulesets" --paginate --jq ".[] | select(.name == \"$name\") | .id" | sed -n '1p')
  if [[ -n "$id" ]]; then
    echo "Updating ruleset $name for $repository"
    gh api --method PUT "repos/${repository}/rulesets/${id}" --input - <<<"$payload"
  else
    echo "Creating ruleset $name for $repository"
    gh api --method POST "repos/${repository}/rulesets" --input - <<<"$payload"
  fi
}

configure_ruleset "main-release-governance" "$branch_payload"
configure_ruleset "release-tag-governance" "$tag_payload"

# Refresh the committed baseline from what the server actually stored, not from
# the payload just sent. The two are not the same thing: GitHub fills in
# defaults (`required_reviewers: []` among them), and the baseline has to match
# what the drift check will read back, or the next Monday's run reports drift
# against this very change.
#
# This step is also what makes an existing claim true. The governance docs say
# the active Ruleset API response is exported and committed; until now this
# script sent the payload and exported nothing, leaving the export as a manual
# snippet in .github/rulesets/README.md that was easy to forget. Under
# ADR-0016's rule a clause is either machine-enforced or a plain disclosure,
# and "someone will remember to run the snippet" is neither.
#
# The operator running --apply holds Administration (write) by definition, so
# the export sees bypass_actors here and the shared canonicalizer's completeness
# check passes. If it ever does not, the failure is the useful kind: it means
# the ruleset was changed by a token that cannot read back what it wrote.
baseline_dir="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)/.github/rulesets"
echo "Refreshing the committed baseline in ${baseline_dir}"
rm -f "${baseline_dir}"/*.json
REPO="$repository" bash "$(dirname "${BASH_SOURCE[0]}")/ruleset-baseline.sh" --export-all "$baseline_dir"
echo "Commit the refreshed baseline in the same pull request as this change."
