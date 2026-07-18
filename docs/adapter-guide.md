---
title: Adapter Guide
sidebar_position: 5
---

# Adapter Guide

This guide is for developers connecting an external core banking, wallet, or
transaction-processing system to Merlon. It covers what an adapter is, how to
read and write an adapter configuration, and how ingested data reaches CDD
scoring.

## What an adapter is

Merlon's Go API does not embed system-specific integration code. Instead, the
**Adapter Isolation** design principle (see [Architecture](./architecture.md))
requires that all external-system differences — field names, authentication
schemes, pagination — are absorbed in a dedicated adapter layer
(`api/internal/adapter/` in the repository) rather than leaking into domain
logic, HTTP handlers, or the native evaluation engine.

An adapter is configured, not coded: you describe an external REST API in a
YAML file, and the adapter layer uses that description to fetch customer and
transaction records and normalize them into Merlon's internal shapes. This
keeps custom integration logic outside the application binary and out of the
BSL-licensed codebase, in line with the **Configuration as the Product**
principle.

Set `MERLON_ADAPTER_CONFIG_PATH` to the path of your adapter configuration
file. See [Configuration Reference](./configuration.md) for how the API loads
environment variables, and `adapters.outbound_allowlist` /
`adapters.block_private_ip_ranges` in `config.example.yaml` for the outbound
network security controls described below.

## Anatomy of an adapter configuration

The repository ships a worked example at `adapters/example_core_banking.yaml`.
Copy it and adjust it for your environment. Every field below is read by the
adapter loader (`api/internal/adapter/config.go`).

```yaml
type: rest
base_url: https://core.example.com/api/v1
timeout_seconds: 30

auth:
  type: bearer
  token_env: CORE_API_TOKEN

endpoints:
  fetch_customer:
    method: GET
    path: /customers/{id}
    field_mapping:
      external_id: "$.customer_id"
      name: "$.full_name"
      country: "$.address.country_code"
      customer_type: "$.type"

  fetch_transactions:
    method: GET
    path: /transactions
    params:
      account_id: "{account_id}"
      since: "{last_sync_timestamp}"
    response_root: "$.transactions"
    field_mapping:
      external_id: "$.txn_id"
      amount: "$.amount"
      currency: "$.currency"
      type: "$.transaction_type"
      base_currency_equivalent: "$.base_currency_equivalent"
```

### Top-level fields

| Field | Meaning |
|---|---|
| `type` | The adapter transport. Only `rest` is currently supported; any other value fails validation. |
| `base_url` | The external system's base URL. Must parse as a valid URL. |
| `timeout_seconds` | Per-request HTTP timeout. Defaults to `30` if unset or non-positive. |
| `auth` | Authentication settings, described below. |
| `endpoints` | A map of named endpoints. At least one is required. |

### Authentication

`auth.type` selects one of four modes, validated in
`api/internal/adapter/config.go`:

| `auth.type` | Required fields | Behavior |
|---|---|---|
| `none` | — | No `Authorization` header is sent. |
| `bearer` | `token_env` | Reads the bearer token from the named environment variable and sends `Authorization: Bearer <token>`. |
| `basic` | `username_env`, `password_env` | Reads a username/password pair from environment variables and sends HTTP Basic auth. |
| `header` | `header_name`, `header_val_env` | Reads a value from the named environment variable and sends it under a custom header name. |

Credential **values** are never written into the adapter YAML file itself —
only the names of environment variables that hold them. This keeps secrets out
of source control while keeping the mapping of "which secret this adapter
uses" in a reviewable, versioned file.

### Endpoints and field mapping

Each entry under `endpoints` is a named operation. The adapter layer
recognizes two endpoint names with a defined contract: `fetch_customer` and
`fetch_transactions`.

- `method` / `path` — the HTTP method and URL path. `path` may contain
  `{param}` placeholders substituted from path parameters (for example `{id}`
  in `fetch_customer`).
- `params` — query-string parameters. Each value is a template that may
  contain `{param}` placeholders substituted from the parameters passed to the
  fetch call (for example `{account_id}` and `{last_sync_timestamp}` in
  `fetch_transactions`).
- `response_root` — a `$.`-prefixed dot-path into the JSON response locating
  the array to iterate. Required for `fetch_transactions`, since transaction
  responses are lists; `fetch_customer` expects a single JSON object at the
  response root and does not use this field.
- `field_mapping` — a map from Merlon's internal field name to a `$.`-prefixed
  dot-path into the (or each) response object. At least one mapping is
  required per endpoint.

Field extraction (`api/internal/adapter/fieldmap.go`) walks the dot-path
segment by segment through the decoded JSON object. A path that does not
resolve — a missing key, or a path passing through a non-object value — simply
omits that field from the result rather than failing the whole fetch. Design
your mappings so that fields your deployment depends on are also fields your
external system reliably returns.

For `fetch_customer`, the recognized internal fields are `external_id`,
`name`, `country`, and `customer_type`. For `fetch_transactions`, they are
`external_id`, `amount`, `currency`, and `type`. Any additional mapped fields
(such as `base_currency_equivalent` in the example) are preserved in the raw
fields map alongside the recognized ones rather than being dropped.

## Outbound network security

Adapters make outbound HTTP calls to systems you configure, so the adapter
layer applies its own network controls independent of what the target system
enforces (`api/internal/adapter/security.go`):

- Non-`https` URLs are rejected unless explicitly allowed for development.
- If `adapters.outbound_allowlist` (`SecurityConfig.OutboundAllowlist`) is
  non-empty, only listed hostnames are permitted.
- If `adapters.block_private_ip_ranges` is enabled, requests are refused when
  the target hostname resolves — directly or via DNS — to a loopback,
  private, link-local, or unspecified IP address. This check is re-applied at
  connection time (`newSafeTransport`), not only at config-validation time, to
  resist DNS-rebinding.

Set these under the `adapters:` section of your deployment's `config.yaml`
(see `config.example.yaml` for the shape). Treat `block_private_ip_ranges` as
required in any deployment where the adapter target is not fully trusted.

## Data flow: adapter to scoring

Once configured, an adapter's `fetch_customer` and `fetch_transactions`
operations return normalized `CustomerData` / `TransactionData` values
(`api/internal/adapter/adapter.go`). The path from there into the rest of
Merlon follows the architecture described in [Architecture](./architecture.md):

1. Your ingestion process calls the adapter to fetch records from the
   external system, using the field mappings above to normalize them.
2. Normalized records are submitted to the Go API's REST endpoints (customer
   and transaction creation/update), which persist them and enforce domain
   validation.
3. The Go API invokes its in-process evaluation engine to compute or update the
   CDD score and evaluate transaction-monitoring scenarios against the
   newly-ingested data, following the score-driven relationship between CDD
   score and TM thresholds described in ADR-0004 (Score-Driven Architecture;
   see [Architecture](./architecture.md)).

See [API Reference](./api/openapi.md) for the REST schema of customer and
transaction records. The engine-facing contract is internal to the Go process;
the public REST contract is documented in the OpenAPI reference.

## Validating and testing an adapter

Before pointing an adapter at a production system:

1. **Validate configuration structure.** `AdapterConfig.Validate()` checks
   that `type` is `rest`, `base_url` parses, `auth` has the fields its type
   requires, and every endpoint has a `method`, `path`, and at least one
   `field_mapping` entry.
2. **Dry-run connectivity.** The adapter layer's `DryRun` operation
   (`api/internal/adapter/dryrun.go`) checks, in order: configuration
   validity, TCP reachability of the parsed `base_url` host, and reports one
   status per configured endpoint. It does not exercise authentication
   against the live system beyond confirming an auth provider could be
   constructed — treat a passing dry run as a connectivity and configuration
   check, not a functional guarantee.
3. **Exercise field mapping against real payloads** by capturing a sample
   response from the external system and confirming the expected internal
   fields are extracted, especially for nested or optional fields.

## Operational notes

- **Idempotency**: the adapter layer itself does not deduplicate records; it
  returns whatever the external system's response contains for each call. If
  your external system does not guarantee non-overlapping result sets between
  polls (for example, a `since` window with clock skew), rely on the
  Merlon API's own external-ID handling on the customer/transaction endpoints
  to avoid duplicate records, rather than assuming the adapter itself
  deduplicates.
- **Error handling**: adapter calls fail closed. A non-2xx HTTP response,
  a JSON decode failure, a missing required `response_root`, or a URL that
  fails the outbound security checks all return an error rather than partial
  data (`api/internal/adapter/rest.go`). Response bodies are capped at 10 MiB
  when read, and error messages include only a truncated preview of the
  response body to avoid leaking large or sensitive payloads into logs.
- **Timeouts**: every HTTP request is subject to `timeout_seconds`
  (default `30`). Set this according to the external system's expected
  latency; a fetch that exceeds the timeout fails as a request error, not a
  partial result.
