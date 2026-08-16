---
sidebar_position: 3
title: Configuration Reference
---

# Configuration Reference

Merlon is configured with environment variables. Copy `.env.example` to `.env`
for local development; do not use its credentials or secrets in production.

## Environment variables

| Variable | Default | Production guidance |
|---|---|---|
| `MERLON_ENV` | `development` | Set to `production`. |
| `MERLON_MODE` | `all` | `api` owns HTTP/realtime work, `worker` owns recovery/TM batch/backtests, `all` runs both. |
| `MERLON_HTTP_ADDR` | `:8080` | Bind behind a TLS-terminating reverse proxy. |
| `MERLON_WORKER_HTTP_ADDR` | `:8081` | Control/health listener used when `MERLON_MODE=worker`. |
| `MERLON_WORKER_CONCURRENCY` | `4` | Worker evaluation concurrency; keep bounded to the database/CPU budget. |
| `MERLON_DATABASE_URL` | unset | Use TLS (`sslmode=require` or stronger) and a least-privilege application role. |
| `MERLON_BACKUP_DATABASE_URL` | unset | Dedicated read-only backup connection used only by `make backup`. Grant the documented existing/future table and sequence read privileges; never substitute a serving or schema-owner URL. |
| `MERLON_MIGRATION_DATABASE_URL` | unset | Separate schema/object-owner connection used by `make migrate`, `make restore`, and `make audit-harden`. This role must manage and have `CREATE` on the target `public` schema. When another role owns a fresh restore database, its owner must transfer `public` to this role and pre-grant direct database `CONNECT` to both this role and the application role. Never substitute the serving-role URL. |
| `MERLON_MIGRATION_BASELINE` | unset | Explicit last-applied migration filename for a pre-ledger database; never inferred automatically. |
| `MERLON_MIGRATIONS_DIR` | `migrations` | Migration command only; directory containing versioned SQL migrations. The `--migrations-dir` flag takes precedence. |
| `MERLON_ENCRYPTION_KEY_RING` | unset | Required for production PII protection. Use the documented key-ring format accepted by `merlon-keyrotate`; loss of every referenced key makes historical encrypted values unrecoverable. Back up keys through a protected KMS or secret manager. |
| `MERLON_INBOUND_WEBHOOK_SECRET` | unset | HMAC secret for durable customer/transaction push webhooks. Keep it in a secret manager; inbound endpoints reject requests while it is unset. |
| `MERLON_JWT_PRIVATE_KEY_FILE` / `MERLON_JWT_PUBLIC_KEY_FILE` | unset | Use an RS256 key pair for local-user authentication. |
| `MERLON_JWT_SECRET` | unset | Development fallback only. Do not set in production when using local-user authentication. |
| `MERLON_BOOTSTRAP_TOKEN` | unset | One-time setup secret. Rotate or remove immediately after the first administrator/API key is created. |
| `MERLON_POSTGRES_PASSWORD` | unset | Compose-only development password. Use a secret manager in production. |
| `MERLON_API_HOST_PORT` | `8080` | Compose-only host port for the API. The standard topology binds it on the host; the demo topology limits it to `127.0.0.1`. The container always listens on `8080`. |
| `MERLON_DB_HOST_PORT` | `5432` | Compose test overlay only. Publishes PostgreSQL on `127.0.0.1`; the standard and demo topologies do not publish a database host port. |
| `MERLON_AUTH_ENABLED` | `false` | Must be `true` in production. |
| `MERLON_SEED` | `false` | Development/demo data only; must be `false` in production. |
| `MERLON_DEMO_DATA_DIR` | unset | Directory holding a full generated demo dataset, loaded when `MERLON_SEED` is enabled. Falls back to the built-in sample if the directory is incomplete. Development/demo only. |
| `MERLON_CONFIG_PATH` | `config.yaml` | Path to application configuration. |
| `MERLON_CACHE_BACKEND` | `memory` | Select the configured cache backend. |
| `MERLON_EVENT_BUS` | `pg_notify` | Event bus driver used with PostgreSQL. |
| `MERLON_RATE_LIMIT` | `0` | Optional per-process requests per minute per resolved client IP. Use only as defense in depth; enforce deployment-wide limits at the trusted ingress. In production, a non-zero value requires `MERLON_TRUSTED_PROXY_CIDRS`. |
| `MERLON_TRUSTED_PROXY_CIDRS` | unset | Comma-separated, narrow CIDRs of reverse proxies allowed to supply `X-Forwarded-For`. Untrusted peers and malformed trusted-side hops fall back to the direct peer address; `/0` is rejected. Configure this even with the application limiter disabled when audit records need the original client IP. |
| `MERLON_ADAPTER_CONFIG_PATH` | unset | Path to an operator-managed adapter configuration. |
| `MERLON_CDD_REVIEW_POLICY_PATH` | `content/cdd_review_policy_v1.yaml` | Versioned CDD periodic-review schedule. Keep the policy file under change control; its digest is exposed in system status. |
| `MERLON_UI_DIR` | unset | Optional directory containing the built UI. |
| `MERLON_SCREENING_IMPORT_ENABLED` | `false` | Enable external sanctions-list imports only with approved endpoints. |
| `MERLON_SCREENING_RESCREEN_ENABLED` | `false` | Enable periodic customer rescreening. |
| `MERLON_SCREENING_IMPORT_INTERVAL` | `24h` | Screening-list import interval. |
| `MERLON_SCREENING_CHECK_INTERVAL` | `1h` | Rescreening interval. |
| `MERLON_SCREENING_OFAC_URL` / `MERLON_SCREENING_EU_URL` | unset | Approved OFAC/EU list endpoints. |
| `MERLON_SCREENING_UN_URL` / `MERLON_SCREENING_MOF_URL` | unset | Approved UN/MOF list endpoints. |
| `MERLON_SCREENING_PEP_URL` | unset | Approved PEP provider endpoint. |
| `MERLON_SMTP_HOST` / `MERLON_SMTP_PORT` | unset / `587` | SMTP endpoint for notifications; configure TLS and credentials. |
| `MERLON_SMTP_USERNAME` / `MERLON_SMTP_PASSWORD` | unset | SMTP credentials; use a secret manager. |
| `MERLON_SMTP_FROM` / `MERLON_SMTP_TO` | unset | Notification sender and comma-separated recipients. |
| `MERLON_SMTP_USE_TLS` | `false` | Enable TLS for SMTP. |
| `MERLON_NOTIFY_ROUTING_PATH` | unset | Optional notification routing YAML path. |
| `MERLON_PUBLIC_URL` | unset | Public base URL used in notification links. |
| `MERLON_WHITELIST_MAX_VALID_DAYS` | `365` | Maximum whitelist validity period. |
| `MERLON_EDD_STAGE2_DAYS` / `MERLON_EDD_STAGE3_DAYS` | `60` / `90` | EDD escalation thresholds. |
| `MERLON_TM_SCENARIOS_PATH` | `tm_scenarios` | Store the directory in controlled source management. Runtime digests identify loaded content but do not authorize changes. |
| `MERLON_OPERATOR_TEAMS` | unset | Comma-separated durable assignment-team directory. Queue rows are never scanned to infer teams; authenticated assignment requires a configured value. |
| `MERLON_CASE_PRIORITY_PATH` | `content/case_priority_v1.yaml` | Versioned YAML mapping from persisted CDD tier/score to case priority. |
| `MERLON_KYC_REQUIRED_FIELDS_PATH` | `content/kyc_required_fields_v1.yaml` | Versioned YAML naming the identity fields each customer type must carry. `enforcement: warn` reports a gap without refusing the write; `reject` refuses it. |
| `MERLON_EDD_POLICY_PATH` | `content/edd_policy_v1.yaml` | Versioned YAML holding the whole EDD stage schedule, due boundary, completion requirements, and tier-downgrade behaviour. Supersedes `MERLON_EDD_STAGE2_DAYS` / `MERLON_EDD_STAGE3_DAYS`. |
| `MERLON_SLA_POLICY_PATH` | `content/sla_policy_v1.yaml` | Versioned YAML holding the optional SLA deadline rules. When unset or empty, SLA reporting remains `not_configured` rather than inventing a due date. |
| `MERLON_CDD_RULE_SELECTION_PATH` | `content/cdd_rule_selection_v1.yaml` | Versioned YAML mapping customer type, product, and jurisdiction to the applicable CDD rule set, so selection is configuration rather than list order. |
| `MERLON_TRAVEL_RULE_POLICY_PATH` | `content/travel_rule_v1.yaml` | Versioned YAML holding the Travel Rule threshold, covered activity, required evidence, exemption reason codes, and whether a conflicting caller assertion is recorded or rejected. |
| `MERLON_SCREENING_READINESS_PATH` | `content/screening_readiness_v1.yaml` | Versioned YAML naming the expected watchlist sources, their freshness windows, and whether an unready required source marks runs degraded or blocks them. |
| `MERLON_CDD_WEIGHTS_PATH` | `cdd_weights.yaml` | Native Go CDD rule root; pin and review content changes. |
| `MERLON_COUNTRY_RISK_PATH` | unset | Optional native Go country-risk table. |
| `MERLON_SCREENING_LISTS_PATH` | `screening_lists` | Native Go last-good screening-list snapshot root. |
| `MERLON_SCREENING_THRESHOLD` | engine default | Name-match score, `0`–`1`, at or above which a screening hit is raised. Lowering it raises recall and review volume. Values outside the range, or that do not parse, are ignored and leave the default in force. Changing it alters detection sensitivity and should follow the same review as a rule change. |
| `MERLON_TM_BASE_CURRENCY` | `JPY` | Interim PH9 invariant: mixed/non-base TM aggregation is fail-alerted to `PENDING_REVIEW`; full FX/decimal semantics are PH10. |
| `MERLON_REALTIME_MONITOR_TIMEOUT` | `30s` | Maximum synchronous history-loading and realtime-monitoring duration before fail-alert queueing. |
| `MERLON_TM_BATCH_SCHEDULE` | `02:00` | Daily `HH:MM` time for transaction-monitoring batch evaluation. |
| `MERLON_TM_BATCH_TIMEZONE` | local timezone | Set an IANA timezone explicitly in production. |
| `MERLON_LOG_LEVEL` | `info` | Keep `info` or stricter; do not use debug logging for sensitive workloads. |

The native Go engine also loads CDD weights and screening content from operator-supplied
paths. Those files are outside the database audit trail. Control them through
source control, change approval, access control, backup, and deployment
procedures. See ADR-0012.

## Encryption key rotation

`merlon-keyrotate` accepts `--key-ring-env` (default
`MERLON_ENCRYPTION_KEY_RING`) to select the environment variable containing
the key-ring specification. Introduce a new active key while retaining old
keys needed to decrypt existing data, deploy the updated ring, complete the
migration, and only then retire the old key. Test restoration of both the
application database and key material before relying on a backup.

## Operational thresholds

Sample TM and screening rules are examples, not production defaults. Choose
thresholds from the deploying organization's risk assessment, test the change,
and approve it through its rule-governance process. In particular, lowering a
screening match threshold increases false positives; raising it increases
missed-match risk. Under the fail-alert principle, resolve uncertainty toward
additional review rather than suppressed alerts.
