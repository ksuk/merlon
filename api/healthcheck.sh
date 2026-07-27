#!/bin/sh
set -eu

# The worker owns a separate HTTP listener. Select the same address as the Go
# process, then turn net/http's listen-address syntax into a URL-safe probe
# target. Wildcard listeners are probed through the matching loopback family.
case "${MERLON_MODE:-all}" in
  worker)
    listen_addr=${MERLON_WORKER_HTTP_ADDR:-:8081}
    ;;
  api|all)
    listen_addr=${MERLON_HTTP_ADDR:-:8080}
    ;;
  *)
    echo "healthcheck: MERLON_MODE must be api, worker, or all" >&2
    exit 1
    ;;
esac

case "$listen_addr" in
  :*)
    port=${listen_addr#:}
    probe_host=127.0.0.1
    ;;
  \[*\]:*)
    bracketed_host=${listen_addr%%]*}
    host=${bracketed_host#\[}
    suffix=${listen_addr#*]}
    case "$suffix" in
      :*) port=${suffix#:} ;;
      *)
        echo "healthcheck: invalid listener address: $listen_addr" >&2
        exit 1
        ;;
    esac
    case "$host" in
      ''|*[!0-9A-Fa-f:.]*)
        echo "healthcheck: invalid listener address: $listen_addr" >&2
        exit 1
        ;;
      ::) probe_host='[::1]' ;;
      *) probe_host="[$host]" ;;
    esac
    ;;
  *:*)
    host=${listen_addr%:*}
    port=${listen_addr##*:}
    case "$host" in
      ''|*[!A-Za-z0-9._-]*)
        echo "healthcheck: invalid listener address: $listen_addr" >&2
        exit 1
        ;;
      0.0.0.0) probe_host=127.0.0.1 ;;
      *) probe_host=$host ;;
    esac
    ;;
  *)
    echo "healthcheck: invalid listener address: $listen_addr" >&2
    exit 1
    ;;
esac

case "$port" in
  ''|*[!0-9]*)
    echo "healthcheck: invalid listener port: $port" >&2
    exit 1
    ;;
esac
if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
  echo "healthcheck: invalid listener port: $port" >&2
  exit 1
fi

exec wget --spider -q "http://$probe_host:$port/healthz/live"
