---
title: Partitioning and Capacity Operations Guide
---

# Partitioning and Capacity Operations Guide

This guide summarizes the operational policy for the four tables that grow
without bound during operation — `transactions`, `audit_logs`,
`customer_score_history`, and `alerts` — based on the currently implemented
table structure and their real-world growth characteristics.

For the DDL templates themselves, see:

- `transactions` / `audit_logs`: ADR-0010, `docs/compliance/data-retention.md`
- `customer_score_history` / `alerts`: ADR-0011, `docs/compliance/data-retention.md`

**This repository's migrations (`migrations/*.sql`) never retrofit
partitioning onto tables that are already live in production.** What
follows is guidance for a new deployment building its environment from
scratch, plus the general procedure a deployment already running
non-partitioned tables would follow if it later migrates to partitioning.

## 1. Migrating an already-live table to partitioning (reference)

To migrate a non-partitioned table to declarative partitioning
(`PARTITION BY RANGE`), the following steps are typical, since PostgreSQL
cannot partition an existing table in place. This repository does not
provide a migration that automates this procedure — it is disruptive work
that requires downtime or a dual-write period, so the decision of whether
and when to run it is left to the deploying organization.

1. **Create the new table**: create a new partitioned parent table under a
   name such as `<table>_partitioned`, and pre-create the monthly partitions
   you need.
2. **Write in parallel**: for a period, replicate writes to the existing
   table into the new table as well, using triggers or dual writes in the
   application.
3. **Migrate the data**: run
   `INSERT INTO <table>_partitioned SELECT * FROM <table>` in batches (for
   example, split by primary-key range or date range) to avoid lock
   contention.
4. **Verify consistency**: confirm the old and new tables match, using row
   counts, checksums, or similar checks.
5. **Cut over**: within a maintenance window, rename the tables to swap them,
   e.g. `ALTER TABLE <table> RENAME TO <table>_old; ALTER TABLE
   <table>_partitioned RENAME TO <table>;`. Create foreign keys, indexes, and
   grants on the new table ahead of time.
6. **Keep the old table**: retain the old table for a verification period
   (for example, one to two weeks) after cutover, and only drop it once
   you've confirmed there are no problems.

## 2. GIN index operator classes

For JSONB columns such as `attributes` / `metadata` / `definition`, prefer
the `jsonb_path_ops` operator class (it produces a smaller index than the
default GIN operator class and suits equality-heavy queries).

```sql
CREATE INDEX idx_customers_attributes_path_ops
    ON customers USING GIN (attributes jsonb_path_ops);
```

For a high-write table like `transactions`, the update cost of a GIN index
is not negligible, so rather than indexing the whole JSONB column, index
only the specific fields that need frequent lookups, using expression
indexes.

```sql
CREATE INDEX idx_transactions_counterparty_country
    ON transactions ((metadata->>'counterparty_country'));
```

## 3. Capacity planning

As a sustained throughput target, use **100 transaction ingestions and
evaluations per second on a standard configuration** as the baseline. Reproduce
that result for each release candidate with the localhost-only
[performance evidence harness](./performance-evidence.md), and size the
deployment from evidence captured on equivalent resources. If you expect load
beyond the measured capacity, plan a migration to a horizontally scalable
setup such as Kubernetes.

## 4. Read-replica routing policy

When running a DB replica configuration (AVAIL-002), separate routing by
query type as follows:

| Query type | Routed to | Reason |
|---|---|---|
| Read-only queries: reporting, detection analytics, backtesting, etc. | Read replica | Spreads load off the primary |
| Real-time TM/CDD/screening evaluation | Primary | Avoids inconsistent evaluation results caused by replication lag |

## 5. Recommended autovacuum tuning

For high-write tables (`transactions`, `alerts`, `customer_score_history`),
we recommend self-hosted deployments tune
`autovacuum_vacuum_scale_factor` below the default (0.2), to **0.02–0.05**,
to keep bloat from accumulating.

```sql
ALTER TABLE transactions SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE alerts SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE customer_score_history SET (autovacuum_vacuum_scale_factor = 0.02);
```

## 6. Handling archived partitions

- `customer_score_history` rows are only physically deleted once they've
  satisfied both the deploying organization's configured retention period
  and the 30-day grace period. If you detach/drop partitions as part of
  operations, confirm ahead of time that doing so does not shorten any
  row's individual retention deadline.
- `alerts` underpin case management and audit evidence, so keep detached
  partitions as an archive rather than dropping them.
- For how `transactions` and `audit_logs` are handled once their retention
  period elapses, see `docs/compliance/data-retention.md` and RET-002/003.
