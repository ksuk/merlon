---
title: Performance Evidence
---

# Performance Evidence

Merlon includes a localhost-only Go harness for reproducing transaction
ingestion and synchronous monitoring measurements against a running release
candidate. The harness sends `POST /api/v1/transactions`, so each successful
request includes the transaction write, its audit mutation, and the realtime
monitoring pass completed before the response.

This is release evidence, not a universal benchmark. Record the host and
container resources with the output, and do not apply one machine's result to
a differently sized deployment.

## Safety and data scope

The command accepts only `localhost` or an IP address for which Go reports
`IsLoopback`. It disables environment HTTP proxies and validates every redirect
destination. A non-loopback destination, embedded URL credential, non-HTTP
scheme, or base path is rejected before any request is sent.

The harness creates its own customers and transactions from fixed synthetic
fixtures. Run it only against a fresh, dedicated database because it does not
delete those records afterwards. Never point it at a production database.

## Prerequisites

1. Check out the exact release-candidate commit and record its full SHA.
2. Start the standard topology on a loopback port with a fresh database. Build
   the image with that SHA as `REVISION`; the harness refuses a target whose
   live `/api/v1/system/status` commit differs.
3. Configure the transaction-monitoring engine with repository-provided
   synthetic policy fixtures and confirm that the live system status reports
   `api`, `database`, and `engine` as configured and `ready`. The harness
   refuses to measure a target when any of those components is missing or not
   ready.
4. Complete setup and create a temporary Analyst API key. Keep the value only
   in `MERLON_PERF_BEARER_TOKEN`; the report never contains it. Leave the
   variable unset only when testing the local auth-disabled demo topology.
5. Record host CPU, memory, OS, Docker version, image digest, PostgreSQL
   version, and container resource limits beside the JSON output. The harness
   records its own Go runtime environment but cannot infer host limits.

One reproducible build sequence is:

```bash
release_commit="$(git rev-parse HEAD)"
docker compose build \
  --build-arg VERSION="v0.0.1-candidate" \
  --build-arg REVISION="$release_commit" \
  --build-arg BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
docker compose up --no-build --detach --wait
```

Use a dedicated Compose project name and a non-default loopback host port when
other local stacks are running. Follow the standard deployment and setup
documentation for the required development credentials; do not write them into
the evidence file or shell history.

## Run

After exporting the temporary API key without printing it:

```bash
make performance-evidence \
  PERF_BASE_URL="http://127.0.0.1:18055" \
  PERF_EXPECTED_COMMIT="$release_commit" \
  PERF_REQUESTS=1000 \
  PERF_CONCURRENCY=16 \
  PERF_CUSTOMERS=16 \
  PERF_WARMUP=100 \
  > performance-evidence.json
```

Setup customer requests and warmup transactions are excluded from the measured
window. Measured transactions are distributed across the synthetic customers
to represent concurrent portfolio activity instead of serial traffic on one
customer.

The JSON report records:

- the target version, exact commit, build timestamp when available,
  authentication mode, base currency, and required-component readiness;
- the harness build and Go runtime environment;
- customer, warmup, request, and concurrency counts;
- start/end timestamps and measured duration;
- response status counts, transport errors, error rate, successful throughput,
  and successful-request P50/P95/P99 latency.

Any measured or warmup failure makes the command exit non-zero. Keep the JSON
and the external host/container description together. If the result does not
support wording elsewhere in the documentation, change the wording or improve
the implementation; do not estimate a missing percentile or throughput value.
