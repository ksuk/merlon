#!/usr/bin/env bash
set -euo pipefail

image="${1:?usage: check-release-image.sh IMAGE}"
expected_user="10001:10001"

actual_user="$(docker image inspect --format '{{.Config.User}}' "$image")"
if [[ "$actual_user" != "$expected_user" ]]; then
  echo "release image user is $actual_user; expected $expected_user" >&2
  exit 1
fi

source_policies="$({
  for file in content/*.yaml; do
    basename "$file"
  done
} | sort)"
image_policies="$(docker run --rm --read-only --entrypoint /bin/sh "$image" -eu -c '
  for file in /app/content/*.yaml; do
    test -f "$file"
    basename "$file"
  done
' | sort)"

if [[ "$image_policies" != "$source_policies" ]]; then
  echo "release image policy inventory differs from root-level content YAML" >&2
  diff -u <(printf '%s\n' "$source_policies") <(printf '%s\n' "$image_policies") >&2 || true
  exit 1
fi

docker run --rm --read-only --entrypoint /bin/sh "$image" -eu -c '
  test ! -e /app/content/README.md
  test ! -e /app/content/_sample
  test ! -e /app/content/schema
  test ! -e /app/demo-data
'

container_id=""
cleanup() {
  if [[ -n "$container_id" ]]; then
    docker rm --force "$container_id" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

container_id="$(docker run --detach --read-only --network none \
  --health-interval 1s --health-timeout 5s --health-start-period 0s --health-retries 10 \
  --env MERLON_AUTH_ENABLED=false "$image")"

for _ in $(seq 1 20); do
  state="$(docker inspect --format '{{.State.Status}}' "$container_id")"
  health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container_id")"
  if [[ "$state" != "running" ]]; then
    docker logs "$container_id" >&2
    echo "release image exited before becoming healthy" >&2
    exit 1
  fi
  if [[ "$health" == "healthy" ]]; then
    exit 0
  fi
  sleep 1
done

docker logs "$container_id" >&2
echo "release image did not become healthy under non-root, read-only execution" >&2
exit 1
