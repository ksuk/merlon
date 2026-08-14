---
title: Data Retention Policy
---

# Data Retention Policy

This document gives example configuration centered on the Act on Prevention
of Transfer of Criminal Proceeds (犯罪収益移転防止法, commonly abbreviated
"犯収法" and referred to below as "the Act"), and describes the data
lifecycle design implemented in Merlon. For other jurisdictions (including
GDPR), the deploying organization must evaluate and configure its own
retention policy according to the laws that apply to it and its own retention
obligations. Retention periods per data category are also managed in the
application layer's `retention_policies` table (`migrations/017_retention.sql`)
and must be kept in sync with the values in this document.

## Retention requirements under the Act

| Record type | Retention period | Basis |
|---|---|---|
| Transaction records | 7 years | The Act (from the end of the transaction) |
| Verification records (identity verification) | 7 years | The Act (from the end of the transaction) |

Seven years is represented as `2555` days. This is the seeded value for the
`transaction_data`, `customer_data`, `alert_case_data`, and
`cdd_score_history` rows in the `retention_policies` table (see
[Configuration Reference](../configuration.md) for how retention periods are
administered through that table and its API, rather than through
`config.yaml`).

## Default retention periods (RET-001, RET-002)

The values below are initial defaults, not fixed values. Deploying
organizations must set a positive retention period for every data category in
accordance with applicable law, contractual obligations, audit requirements,
and their own deletion policy. Configuration changes are recorded in the
audit log, and data is physically deleted 30 days after its retention
deadline is reached.

| Data category | Default retention period | Seeded minimum | Reference point | Configurable |
|---|---|---|---|---|
| Transaction data | 7 years (2555 days) | 2555 days | Date the transaction occurred (`transactions.executed_at`) | Yes, at or above the minimum |
| Customer data | 7 years (2555 days) | 2555 days | Date of the last transaction | Yes, at or above the minimum |
| Alerts / cases | 7 years (2555 days) | 2555 days | `resolved_at` / `closed_at` (unresolved/open records are excluded) | Yes, at or above the minimum |
| Audit logs | 10 years (3650 days) | none | Date the log was recorded (`created_at`) | Yes, to any positive number of days |
| CDD score history | 7 years (2555 days) | 2555 days | `scored_at` | Yes, at or above the minimum |

### Minimum retention guardrail

Each `retention_policies` row carries an optional `min_retention_days` lower
bound. The seed data sets it to 2555 days — seven years, matching the Act's
retention obligations — for `transaction_data`, `customer_data`,
`alert_case_data`, and `cdd_score_history`; `audit_log` has no seeded
minimum. The retention API rejects any period below a row's minimum
(`api/internal/server/retention.go`), so a mistyped setting cannot silently
make records inside the statutory window eligible for purge.

The minimum is deployment data, not a fixed rule of the software. A
deployment whose obligations differ — for example, a shorter mandatory
deletion horizon under GDPR or another regime — changes the bound directly
in the database, under its own legal assessment:

```sql
-- Example: permit customer_data retention shorter than seven years
UPDATE retention_policies
SET min_retention_days = 1095   -- or NULL to remove the bound entirely
WHERE data_category = 'customer_data';
```

After the bound is adjusted, the retention API accepts any positive number
of days at or above it.

## Data lifecycle design in Merlon

Under the Auditability First principle, reproducibility of decision rationale
takes top priority.

| Data category | Lifecycle |
|---|---|
| Audit logs | Append-only for the duration of the retention period. Logically deleted once the deadline is reached, then physically deleted 30 days later |
| Customer data | Retained for 7 years after the transaction relationship ends. Once the retention period has elapsed, direct PII within `attributes` can be anonymized in response to an Act on the Protection of Personal Information (個人情報保護法, "APPI") deletion request (RET-004, `api/internal/retention/anonymize.go`) |
| Rule definitions | All versions are retained permanently (never deleted, so that past decisions remain reproducible) |
| Score history | Tracks the evolution of risk ratings for the duration of the configured retention period. Logically deleted once the deadline is reached, then physically deleted 30 days later |
| Transaction data | Logically deleted after the configured retention period, then physically deleted 30 days later |

`PurgeJob` runs daily in PostgreSQL-backed deployments. At each configured
retention cutoff it records `purge_marked_at`, and normal API reads exclude
marked data. After a 30-day grace period it physically deletes marked
transaction data, alert/case data, CDD score history, customer data,
dependent screening results, and audit logs. A customer remains until
independently retained child records complete their own lifecycle.

### Why rule definitions are retained permanently

For the duration of the retention period, the rule definitions and score
history that were in effect at the time make it possible to reproduce the
rationale behind a decision. Overwriting or deleting a rule definition would
destroy the ability to reproduce past decisions, so all versions are kept.
Score history, by contrast, is deleted after the user-configured retention
period plus the 30-day grace period elapses, so reproducibility is not
guaranteed beyond that window.

## Archive strategy (partitioning template for new deployments)

PostgreSQL declarative partitioning (`PARTITION BY RANGE`) is effective for
deployments handling large data volumes, but **this project does not
retrofit partitioning onto tables that are already live in production**
(ADR-0010). The
following is an example DDL template for monthly RANGE partitions that a
newly-provisioned deployment can adopt from scratch.

```sql
-- audit_logs: example monthly partitioning keyed on created_at
CREATE TABLE audit_logs (
    id              BIGSERIAL,
    user_id         VARCHAR(255),
    action          VARCHAR(100) NOT NULL,
    resource_type   VARCHAR(100) NOT NULL,
    resource_id     VARCHAR(255),
    details         JSONB DEFAULT '{}',
    ip_address      INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE audit_logs_2026_07 PARTITION OF audit_logs
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
-- Add a CREATE TABLE ... PARTITION OF ... statement each month going forward
-- (consider an extension such as pg_partman if you want to automate this)

-- transactions: example monthly partitioning keyed on executed_at
CREATE TABLE transactions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id           UUID NOT NULL,
    external_id           VARCHAR(255) NOT NULL,
    amount                NUMERIC(20,2) NOT NULL,
    currency              VARCHAR(3) NOT NULL,
    direction             VARCHAR(10) NOT NULL,
    executed_at           TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
    -- (see migrations/002_transactions_alerts.sql for the actual column list)
) PARTITION BY RANGE (executed_at);

CREATE TABLE transactions_2026_07 PARTITION OF transactions
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
```

Operational guidance:

- For old partitions past the retention period (10 years for audit logs, 7
  years for transactions), move them to cold storage or detach them
  (`DETACH PARTITION`). Before physically deleting anything, the deploying
  organization should implement and verify deletion procedures that meet its
  own legal and audit requirements.
- Operating on individual partitions keeps the cost of applying retention
  policy (range scans, purges) constant even for very large tables.
- A deployment already running non-partitioned tables that wants to migrate
  to partitioning generally needs to follow the sequence "create new table →
  write to both in parallel → migrate data → cut over reads." This migration
  can be disruptive and may involve downtime, so this repository's automated
  migrations do not provide it. Plan any such migration separately.
- Partitioning strategy, capacity planning, and migration guidance for the
  broader set of ever-growing tables, including `customer_score_history` and
  `alerts`, are covered in a separate ADR.

This project does not retrofit partitioning onto tables that are already
live in production
(ADR-0010,
ADR-0011). The following is an
example DDL template for monthly RANGE partitions that a newly-provisioned
deployment can adopt from scratch, covering `customer_score_history` and
`alerts` (see ADR-0010 for `audit_logs`/`transactions`).

```sql
-- customer_score_history: example monthly partitioning keyed on scored_at
CREATE TABLE customer_score_history (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID NOT NULL,
    risk_score   NUMERIC NOT NULL,
    risk_tier    TEXT NOT NULL,
    factors      JSONB,
    rule_set_id  TEXT,
    rule_set_version INTEGER,
    scored_at    TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (scored_at);

CREATE TABLE customer_score_history_2026_07 PARTITION OF customer_score_history
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
-- Add a CREATE TABLE ... PARTITION OF ... statement each month going forward

-- alerts: example monthly partitioning keyed on detected_at
CREATE TABLE alerts (
    id          TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    scenario_id TEXT NOT NULL,
    severity    TEXT NOT NULL,
    status      TEXT NOT NULL,
    score       NUMERIC,
    description TEXT,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (detected_at);

CREATE TABLE alerts_2026_07 PARTITION OF alerts
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
-- Add a CREATE TABLE ... PARTITION OF ... statement each month going forward
```

- Once `customer_score_history` rows pass the user-configured retention
  period, they are physically deleted 30 days after `purge_marked_at` is set.
  When partitioning, confirm the retention policy and grace period before
  detaching an old partition slated for deletion.
- `alerts` underpin case management and audit evidence, so keep detached
  partitions as an archive rather than dropping them.
- For the general procedure a deployment already running non-partitioned
  tables would follow to migrate to partitioning, GIN index operator-class
  selection guidance, capacity planning, read-replica routing policy, and
  recommended autovacuum tuning, see
  [`docs/operations/partitioning-guide.md`](../operations/partitioning-guide.md).

## Legal holds

This release does not provide a per-record legal-hold mechanism. When
records become subject to a preservation obligation — a regulator's inquiry,
litigation, or an internal investigation — that outlasts the configured
retention period, the deploying organization must implement the hold
operationally:

- **Before the retention cutoff**: extend the affected category's retention
  period through the retention API. This is a category-level lever; it keeps
  every record in the category, not only those under hold.
- **After a record has been purge-marked**: extending the retention period
  does **not** rescue records whose `purge_marked_at` is already set — they
  are still physically deleted once the 30-day grace period elapses. Within
  the grace period, either export and preserve the affected records outside
  Merlon under the organization's evidence-management procedures, or clear
  the marks directly, under the organization's own change-control and legal
  assessment:

  ```sql
  -- Example: release purge marks on transactions under a legal hold
  UPDATE transactions SET purge_marked_at = NULL
  WHERE purge_marked_at IS NOT NULL AND customer_id = '<customer under hold>';
  ```

  Records whose marks are cleared will be re-marked by the next purge run
  unless the category's retention period has also been extended, so both
  steps are normally required.

Unresolved alerts and open cases are always excluded from purging, so active
investigations tracked in Merlon are protected without manual steps.

## Limits of audit-log tamper resistance

Merlon provides after-the-fact verification via `merlon-audit verify`. It does
not provide database-enforced immutability, WORM storage, or a cryptographic
hash chain for audit logs. The deploying
organization is responsible for configuring database roles, backups, access
control, and monitoring. See [Regulatory Scope](regulatory-scope.md) for
details.
