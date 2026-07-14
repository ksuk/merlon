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
| `MERLON_MIGRATION_DATABASE_URL` | unset | Migration command only; use a separate schema-owner connection. Required by `make migrate` in production. |
| `MERLON_MIGRATION_BASELINE` | unset | Explicit last-applied migration filename for a pre-ledger database; never inferred automatically. |
| `MERLON_ENCRYPTION_KEY_RING` | unset | Required for production PII protection. Use the documented key-ring format accepted by `merlon-keyrotate`; loss of every referenced key makes historical encrypted values unrecoverable. Back up keys through a protected KMS or secret manager. |
| `MERLON_JWT_PRIVATE_KEY_FILE` / `MERLON_JWT_PUBLIC_KEY_FILE` | unset | Use an RS256 key pair for local-user authentication. |
| `MERLON_JWT_SECRET` | unset | Development fallback only. Do not set in production when using local-user authentication. |
| `MERLON_BOOTSTRAP_TOKEN` | unset | One-time setup secret. Rotate or remove immediately after the first administrator/API key is created. |
| `MERLON_POSTGRES_PASSWORD` | unset | Compose-only development password. Use a secret manager in production. |
| `MERLON_AUTH_ENABLED` | `false` | Must be `true` in production. |
| `MERLON_SEED` | `false` | Development/demo data only; must be `false` in production. |
| `MERLON_CONFIG_PATH` | `config.yaml` | Path to application configuration. |
| `MERLON_CACHE_BACKEND` | `memory` | Select the configured cache backend. |
| `MERLON_EVENT_BUS` | `pg_notify` | Event bus driver used with PostgreSQL. |
| `MERLON_RATE_LIMIT` | `0` | Requests per window; `0` disables the limiter. |
| `MERLON_ADAPTER_CONFIG_PATH` | unset | Path to an operator-managed adapter configuration. |
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
| `MERLON_CDD_WEIGHTS_PATH` | `cdd_weights.yaml` | Native Go CDD rule root; pin and review content changes. |
| `MERLON_COUNTRY_RISK_PATH` | unset | Optional native Go country-risk table. |
| `MERLON_SCREENING_LISTS_PATH` | `screening_lists` | Native Go last-good screening-list snapshot root. |
| `MERLON_TM_BASE_CURRENCY` | `JPY` | Interim PH9 invariant: mixed/non-base TM aggregation is fail-alerted to `PENDING_REVIEW`; full FX/decimal semantics are PH10. |
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
