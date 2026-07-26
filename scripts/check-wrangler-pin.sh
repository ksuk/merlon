#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

# cloudflare/wrangler-action installs the version named in wranglerVersion
# rather than the one resolved from website/package-lock.json, so the deployed
# Wrangler is only the one this repository declares while the two agree.
manifest_file=website/package.json
workflow_file=.github/workflows/docs-deploy.yml

manifest_ref=$(
  sed -nE 's/^[[:space:]]*"wrangler":[[:space:]]*"([^"]+)".*$/\1/p' "$manifest_file"
)
if [[ $(wc -l <<<"$manifest_ref") -ne 1 || -z $manifest_ref ]]; then
  echo "$manifest_file must declare exactly one \"wrangler\" dependency" >&2
  exit 1
fi
if [[ ! $manifest_ref =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "$manifest_file must pin wrangler to an exact version (got: $manifest_ref)" >&2
  exit 1
fi

workflow_ref=$(
  sed -nE "s/^[[:space:]]*wranglerVersion:[[:space:]]*'([^']+)'.*$/\1/p" "$workflow_file"
)
if [[ $(wc -l <<<"$workflow_ref") -ne 1 || -z $workflow_ref ]]; then
  echo "$workflow_file must set exactly one wranglerVersion" >&2
  exit 1
fi

if [[ $manifest_ref != "$workflow_ref" ]]; then
  echo "wrangler versions are inconsistent" >&2
  echo "$manifest_file: $manifest_ref" >&2
  echo "$workflow_file: $workflow_ref" >&2
  exit 1
fi

echo "wrangler versions are synchronized: $manifest_ref"
