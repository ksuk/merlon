#!/usr/bin/env bash
set -euo pipefail

apply=false
if [[ "${1:-}" == "--apply" ]]; then
  apply=true
elif [[ $# -ne 0 ]]; then
  echo "usage: $0 [--apply]" >&2
  exit 2
fi

command -v gh >/dev/null || { echo "gh is required" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

repository=${GITHUB_REPOSITORY:-}
if [[ -z "$repository" ]]; then
  repository=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
fi

branch_payload=$(jq -n '{
  name: "main-release-governance",
  target: "branch",
  enforcement: "active",
  conditions: {ref_name: {include: ["refs/heads/main"], exclude: []}},
  rules: [
    {type: "deletion"},
    {type: "non_fast_forward"},
    {type: "required_linear_history"},
    {type: "pull_request", parameters: {
      allowed_merge_methods: ["squash"],
      dismiss_stale_reviews_on_push: true,
      require_code_owner_review: true,
      require_last_push_approval: true,
      required_approving_review_count: 1,
      required_review_thread_resolution: true
    }},
    {type: "required_status_checks", parameters: {
      strict_required_status_checks_policy: true,
      do_not_enforce_on_create: false,
      required_status_checks: [
        {context: "CI / Required"},
        {context: "Security / Required"},
        {context: "DCO / check-signoffs"},
        {context: "Traceability / Required"}
      ]
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
  echo "Dry run: the following active rulesets would be configured for $repository:"
  jq . <<<"$branch_payload"
  jq . <<<"$tag_payload"
  echo "Re-run with --apply after an independent maintainer reviews the payload."
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
