#!/usr/bin/env bash
set -euo pipefail

# Capture the demo UI screenshots committed under docs/img/ and embedded in
# README.md.
#
# The browser runs in a container, not on this machine. Merlon has no browser
# toolchain — no Playwright dependency, no headless Chromium — and adding one
# to the repository to produce three PNGs that change only when the UI changes
# would not pay for itself. Instead a thin image derived from Microsoft's
# Playwright image (browsers already baked in) is run on the demo stack's own
# compose network, scripts/capture-screenshots.mjs is copied in, and the PNGs
# are copied back out. Nothing is bind-mounted and nothing is installed on the
# host.
#
# Talking to the API over the compose network rather than a published port
# means this works whether or not the demo stack publishes 127.0.0.1:8080.
#
# The demo dataset is deterministic, but the images are not byte-identical
# across runs: the UI renders relative timestamps that drift against the wall
# clock. Re-run this when the UI changes, not routinely.
#
# Usage:
#   docker compose -f docker-compose.demo.yml up --build -d
#   scripts/capture-screenshots.sh
#
# Output:
#   docs/img/demo-dashboard.png     embedded in README.md
#   docs/img/demo-customer-cdd.png  embedded in README.md
#   docs/img/demo-case.png          committed for future documentation use

cd "$(dirname "$0")/.."

readonly COMPOSE_FILE=docker-compose.demo.yml
readonly CONTAINER=merlon-screenshots
readonly OUT_DIR=docs/img
readonly CAPTURE_IMAGE=merlon-screenshots:local

# Digest-pinned, per the repository convention that every container image
# reference names a digest as well as a tag. The tag is kept alongside it so a
# human can see which Playwright release the digest belongs to; the npm package
# installed below must match it, because the browsers baked into the image are
# the ones that Playwright release expects.
readonly PLAYWRIGHT_VERSION=1.54.0
readonly PLAYWRIGHT_IMAGE=mcr.microsoft.com/playwright:v1.54.0-noble@sha256:18d6adb6aaccf1b0f30eba890069972e089138e4a59ddb5303d7e7290e4e38b6

# The demo stack has to already be up: this script photographs it, it does not
# start it. Starting it here would hide a failed or half-seeded stack behind a
# set of blank screenshots.
api_container=$(docker compose -f "$COMPOSE_FILE" ps -q api 2>/dev/null || true)
if [[ -z $api_container ]]; then
  echo "The demo API container is not running." >&2
  echo >&2
  echo "Start the demo stack first, and wait for it to report healthy:" >&2
  echo "  docker compose -f $COMPOSE_FILE up --build -d" >&2
  exit 1
fi

# Derived, never hardcoded: the compose network name is the project name plus
# "_default", and the project name follows the directory the repository was
# cloned into.
network=$(docker inspect "$api_container" \
  --format '{{range $k,$_ := .NetworkSettings.Networks}}{{$k}}{{end}}')
if [[ -z $network ]]; then
  echo "Could not determine the compose network for the demo API container." >&2
  exit 1
fi

echo "Using network: $network"
echo "Using image:   $PLAYWRIGHT_IMAGE"

cleanup() {
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Any earlier run that died before its trap fired would leave the name taken.
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true

# The Playwright image ships the browsers but not the Playwright JS library, so
# a thin image is derived from it that adds the library.
#
# The library is added in a build rather than installed into an already-running
# container on purpose. An install performed with docker exec has to be trusted
# on its exit status, and npm has been observed on some storage drivers to
# report success having downloaded and extracted the package without the tree
# ever appearing on disk -- which then surfaces much later, and much less
# legibly, as a module resolution error from node. A build either produces a
# layer containing the library or fails, the `require` below turns a silently
# empty install into a build failure, and the layer is cached, so repeat runs
# skip the download entirely.
echo "Building the capture image from $PLAYWRIGHT_IMAGE"
docker build --quiet -t "$CAPTURE_IMAGE" -f - scripts/ >/dev/null <<DOCKERFILE
FROM $PLAYWRIGHT_IMAGE
# The browsers are already in the image; only the JS library is missing, and it
# has to match the image's Playwright release.
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
WORKDIR /cap
RUN npm install --no-audit --no-fund playwright@$PLAYWRIGHT_VERSION \
 && node -e "require('playwright')"
RUN mkdir -p /out
CMD ["node", "/cap/capture-screenshots.mjs"]
DOCKERFILE

# Created but not started, so the capture script can be copied in before the
# container's command runs. Nothing is bind-mounted: every transfer is docker cp.
docker create --name "$CONTAINER" --network "$network" \
  -e BASE_URL=http://api:8080 -e OUT_DIR=/out \
  "$CAPTURE_IMAGE" >/dev/null

docker cp scripts/capture-screenshots.mjs "$CONTAINER:/cap/capture-screenshots.mjs"

docker start "$CONTAINER" >/dev/null
capture_status=$(docker wait "$CONTAINER")
docker logs "$CONTAINER"

if [[ $capture_status -ne 0 ]]; then
  echo "The capture run failed (exit $capture_status)." >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
docker cp "$CONTAINER:/out/." "$OUT_DIR/"

# A screenshot of a blank page or a loading spinner still writes a valid PNG,
# and it is small. This does not prove the pages rendered correctly, but it
# does catch the failure mode where they rendered nothing at all.
readonly MIN_BYTES=20480
failed=0
for name in demo-dashboard.png demo-customer-cdd.png demo-case.png; do
  file="$OUT_DIR/$name"
  if [[ ! -f $file ]]; then
    echo "missing: $file" >&2
    failed=1
    continue
  fi
  bytes=$(wc -c < "$file")
  if (( bytes < MIN_BYTES )); then
    echo "suspiciously small (${bytes}B < ${MIN_BYTES}B), the page probably did not render: $file" >&2
    failed=1
    continue
  fi
  printf '  %-24s %8d bytes\n' "$name" "$bytes"
done

if (( failed )); then
  exit 1
fi

echo
echo "Screenshots written to $OUT_DIR/"
