#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

readonly COMPOSE_FILE=docker-compose.yml
readonly PROJECT=${MERLON_STANDARD_PROJECT:-merlon-standard-acceptance}
readonly CONTAINER=merlon-standard-acceptance-verifier
readonly VERIFY_IMAGE=merlon-standard-acceptance-verifier:local
readonly PLAYWRIGHT_VERSION=1.54.0
readonly PLAYWRIGHT_IMAGE=mcr.microsoft.com/playwright:v1.54.0-noble@sha256:18d6adb6aaccf1b0f30eba890069972e089138e4a59ddb5303d7e7290e4e38b6
readonly TEMP_ROOT=$(mktemp -d "$PWD/.standard-acceptance.XXXXXX")
readonly CONTENT_ROOT="$TEMP_ROOT/operator-content"

secret() { openssl rand -hex 24; }
export MERLON_POSTGRES_PASSWORD=${MERLON_POSTGRES_PASSWORD:-$(secret)}
export MERLON_BOOTSTRAP_TOKEN=${MERLON_BOOTSTRAP_TOKEN:-$(secret)}
export MERLON_JWT_SECRET=${MERLON_JWT_SECRET:-$(secret)$(secret)}
export MERLON_API_HOST_PORT=${MERLON_STANDARD_PORT:-18083}
export MERLON_BUILD_VERSION=${MERLON_BUILD_VERSION:-v0.0.2-acceptance}
export MERLON_BUILD_REVISION=${MERLON_BUILD_REVISION:-$(git rev-parse HEAD)}
export MERLON_BUILD_BUILT_AT=${MERLON_BUILD_BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
export ADMIN_EMAIL=${MERLON_STANDARD_ADMIN_EMAIL:-admin@acceptance.invalid}
export ADMIN_PASSWORD=${MERLON_STANDARD_ADMIN_PASSWORD:-$(secret)}
export ANALYST_EMAIL=${MERLON_STANDARD_ANALYST_EMAIL:-analyst@acceptance.invalid}
export VIEWER_EMAIL=${MERLON_STANDARD_VIEWER_EMAIL:-viewer@acceptance.invalid}
export USER_PASSWORD=${MERLON_STANDARD_USER_PASSWORD:-$(secret)}

mkdir -p "$CONTENT_ROOT/tm_scenarios" "$CONTENT_ROOT/screening_lists"
cp content/_sample/cdd_weights/funds_transfer.yaml "$CONTENT_ROOT/cdd_weights.yaml"
cp content/_sample/tm_scenarios/*.yaml "$CONTENT_ROOT/tm_scenarios/"
cp deploy/seed/demo/screening_lists/*.yaml "$CONTENT_ROOT/screening_lists/"
if command -v cygpath >/dev/null 2>&1; then
  export MERLON_OPERATOR_CONTENT_PATH=$(cygpath -w "$CONTENT_ROOT")
else
  export MERLON_OPERATOR_CONTENT_PATH=$CONTENT_ROOT
fi

compose=(docker compose -p "$PROJECT" -f "$COMPOSE_FILE")
cleanup() {
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$TEMP_ROOT"
}
trap cleanup EXIT

"${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
"${compose[@]}" up --build --detach

api_container=$("${compose[@]}" ps -q api)
network=$(docker inspect "$api_container" --format '{{range $k,$_ := .NetworkSettings.Networks}}{{$k}}{{end}}')

docker build --quiet -t "$VERIFY_IMAGE" -f - scripts/ >/dev/null <<DOCKERFILE
FROM $PLAYWRIGHT_IMAGE
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
WORKDIR /verify
RUN npm install --no-audit --no-fund playwright@$PLAYWRIGHT_VERSION
CMD ["node", "/verify/verify-standard-acceptance.mjs"]
DOCKERFILE

run_verifier() {
  local phase=$1
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  docker create --name "$CONTAINER" --network "$network" \
    -e BASE_URL=http://localhost:8080 -e UPSTREAM_URL=http://api:8080 -e PHASE="$phase" \
    -e ADMIN_EMAIL -e ADMIN_PASSWORD -e ANALYST_EMAIL -e VIEWER_EMAIL -e USER_PASSWORD \
    -e EXPECTED_VERSION="$MERLON_BUILD_VERSION" -e EXPECTED_REVISION="$MERLON_BUILD_REVISION" \
    "$VERIFY_IMAGE" >/dev/null
  docker cp scripts/verify-standard-acceptance.mjs "$CONTAINER:/verify/verify-standard-acceptance.mjs"
  docker start "$CONTAINER" >/dev/null
  local status
  status=$(docker wait "$CONTAINER")
  docker logs "$CONTAINER"
  [[ $status -eq 0 ]]
}

run_verifier before-restart
"${compose[@]}" restart api
deadline=$((SECONDS + 90))
until [[ $(docker inspect "$api_container" --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}') == healthy ]]; do
  if (( SECONDS >= deadline )); then
    "${compose[@]}" logs api
    echo "Standard API did not become healthy after restart." >&2
    exit 1
  fi
  sleep 2
done
run_verifier after-restart

echo "Standard Compose browser acceptance passed."
