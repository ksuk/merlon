#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

# The release image is built from the toolchain versions in the Dockerfiles,
# while every test that gates that image runs on the versions in the workflows.
# Raising one side without the other publishes an image no CI job exercised, and
# Release Image Dry Run cannot catch it: it proves the image still builds, not
# that the resulting binary passed anything. api/go.mod and the DevContainer
# name the same toolchain again, so both can drift the same way.

dockerfiles=(api/Dockerfile ui/Dockerfile)
workflows=(
  .github/workflows/ci.yml
  .github/workflows/docs-deploy.yml
  .github/workflows/release.yml
  .github/workflows/security.yml
)
devcontainer_file=.devcontainer/devcontainer.json

# image_versions FILE IMAGE -- the tag version of every digest-pinned FROM in
# FILE naming IMAGE, one per line, with the -alpineN.NN suffix removed.
image_versions() {
  sed -nE "s|^FROM[[:space:]]+$2:([0-9][^[:space:]@]*)@sha256:[0-9a-f]{64}([[:space:]].*)?$|\1|p" "$1" |
    sed -E 's/-alpine.*$//'
}

# assert_workflow_version KEY EXPECTED -- every occurrence of KEY across the
# workflows names EXPECTED. A KEY that appears nowhere is an error rather than a
# pass, so renaming or dropping the setup step cannot make the guard vacuous.
assert_workflow_version() {
  local key=$1 expected=$2 file version found=0
  for file in "${workflows[@]}"; do
    while read -r version; do
      found=$((found + 1))
      if [[ $version != "$expected" ]]; then
        echo "$file sets $key: '$version' but the build image pins $expected" >&2
        exit 1
      fi
    done < <(sed -nE "s/^[[:space:]]*$key:[[:space:]]*'([^']+)'.*\$/\1/p" "$file")
  done

  if [[ $found -eq 0 ]]; then
    echo "no $key is set in any workflow; this guard cannot compare anything" >&2
    exit 1
  fi
}

# devcontainer_feature_version FEATURE -- the "version" of the first object
# following the FEATURE key in devcontainer.json.
devcontainer_feature_version() {
  awk -v feature="$1" '
    index($0, feature) { pending = 1; next }
    pending && match($0, /"version"[[:space:]]*:[[:space:]]*"[^"]+"/) {
      value = substr($0, RSTART, RLENGTH)
      sub(/^"version"[[:space:]]*:[[:space:]]*"/, "", value)
      sub(/"$/, "", value)
      print value
      pending = 0
    }
  ' "$devcontainer_file"
}

# assert_single NAME FILE VALUES... -- exactly one value was extracted.
assert_single() {
  local name=$1 file=$2
  shift 2
  if [[ $# -ne 1 || -z ${1:-} ]]; then
    echo "$file must declare exactly one $name (found $#)" >&2
    exit 1
  fi
}

mapfile -t go_refs < <(image_versions api/Dockerfile golang)
assert_single "golang image reference pinned by tag and digest" api/Dockerfile "${go_refs[@]}"
go_version=${go_refs[0]}
if [[ ! $go_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "api/Dockerfile must pin golang to an exact patch version (got: $go_version)" >&2
  exit 1
fi

node_version=
for file in "${dockerfiles[@]}"; do
  mapfile -t node_refs < <(image_versions "$file" node)
  assert_single "node image reference pinned by tag and digest" "$file" "${node_refs[@]}"
  if [[ ! ${node_refs[0]} =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "$file must pin node to an exact patch version (got: ${node_refs[0]})" >&2
    exit 1
  fi
  if [[ -z $node_version ]]; then
    node_version=${node_refs[0]}
  elif [[ ${node_refs[0]} != "$node_version" ]]; then
    echo "node image pins are inconsistent" >&2
    echo "expected: $node_version" >&2
    echo "$file: ${node_refs[0]}" >&2
    exit 1
  fi
done

assert_workflow_version go-version "$go_version"
assert_workflow_version node-version "$node_version"

# The go directive states the module's minimum toolchain, so it carries no
# patch component of its own and is compared at major.minor.
mapfile -t gomod_refs < <(sed -nE 's/^go[[:space:]]+([0-9]+\.[0-9]+)(\.[0-9]+)?[[:space:]]*$/\1/p' api/go.mod)
assert_single "go directive" api/go.mod "${gomod_refs[@]}"
if [[ ${gomod_refs[0]} != "${go_version%.*}" ]]; then
  echo "api/go.mod declares go ${gomod_refs[0]} but the build image pins $go_version" >&2
  exit 1
fi

# The DevContainer features take a version prefix rather than a full version,
# so each is compared against as many leading components as it declares.
mapfile -t devcontainer_go < <(devcontainer_feature_version 'devcontainers/features/go:')
assert_single "go feature version" "$devcontainer_file" "${devcontainer_go[@]}"
if [[ ${go_version%.*} != "${devcontainer_go[0]}" ]]; then
  echo "$devcontainer_file requests Go ${devcontainer_go[0]} but the build image pins $go_version" >&2
  exit 1
fi

mapfile -t devcontainer_node < <(devcontainer_feature_version 'devcontainers/features/node:')
assert_single "node feature version" "$devcontainer_file" "${devcontainer_node[@]}"
if [[ ${node_version%%.*} != "${devcontainer_node[0]}" ]]; then
  echo "$devcontainer_file requests Node.js ${devcontainer_node[0]} but the build image pins $node_version" >&2
  exit 1
fi

echo "toolchain versions are synchronized: Go $go_version, Node.js $node_version"
