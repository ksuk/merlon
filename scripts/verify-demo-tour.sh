#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

readonly COMPOSE_FILE=docker-compose.demo.yml
readonly CONTAINER=merlon-demo-tour-verifier
readonly VERIFY_IMAGE=merlon-demo-tour-verifier:local
readonly PLAYWRIGHT_VERSION=1.54.0
readonly PLAYWRIGHT_IMAGE=mcr.microsoft.com/playwright:v1.54.0-noble@sha256:18d6adb6aaccf1b0f30eba890069972e089138e4a59ddb5303d7e7290e4e38b6

compose=(docker compose -f "$COMPOSE_FILE")
if [[ -n ${MERLON_DEMO_PROJECT:-} ]]; then
  compose+=(-p "$MERLON_DEMO_PROJECT")
fi

api_container=$("${compose[@]}" ps -q api 2>/dev/null || true)
if [[ -z $api_container ]]; then
  echo "The demo API container is not running." >&2
  echo "Start a fresh demo stack and wait for it to become healthy first." >&2
  exit 1
fi

health=$(docker inspect "$api_container" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}')
if [[ $health != healthy ]]; then
  echo "The demo API container is not healthy (status: $health)." >&2
  exit 1
fi

networks=$(docker inspect "$api_container" \
  --format '{{range $k,$_ := .NetworkSettings.Networks}}{{$k}}{{"\n"}}{{end}}' \
  | sed '/^[[:space:]]*$/d')
if [[ -n ${MERLON_DEMO_NETWORK:-} ]]; then
  network=$MERLON_DEMO_NETWORK
elif [[ -z $networks ]]; then
  echo "Could not determine the demo API compose network." >&2
  exit 1
elif [[ $(printf '%s\n' "$networks" | wc -l) -gt 1 ]]; then
  echo "The demo API container has multiple networks; set MERLON_DEMO_NETWORK." >&2
  printf '  %s\n' $networks >&2
  exit 1
else
  network=$networks
fi

cleanup() {
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true

echo "Building verifier from $PLAYWRIGHT_IMAGE"
docker build --quiet -t "$VERIFY_IMAGE" -f - scripts/ >/dev/null <<DOCKERFILE
FROM $PLAYWRIGHT_IMAGE
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
WORKDIR /verify
RUN npm install --no-audit --no-fund playwright@$PLAYWRIGHT_VERSION \
 && node -e "require('playwright')"
CMD ["node", "/verify/verify-demo-tour.mjs"]
DOCKERFILE

docker create --name "$CONTAINER" --network "$network" \
  -e BASE_URL=http://api:8080 "$VERIFY_IMAGE" >/dev/null
docker cp scripts/verify-demo-tour.mjs "$CONTAINER:/verify/verify-demo-tour.mjs"
docker start "$CONTAINER" >/dev/null
status=$(docker wait "$CONTAINER")
docker logs "$CONTAINER"
if [[ $status -ne 0 ]]; then
  echo "The demo tour verification failed (exit $status)." >&2
  exit 1
fi

echo "The documented Docker Demo tours passed."
