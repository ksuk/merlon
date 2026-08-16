---
title: Initial Data Migration
---

# Initial Data Migration

This runbook covers the one-time load of your existing customer master and
transaction history into a fresh Merlon installation, from files exported by
your core banking or ledger system.

## What is supported today

The supported path for an initial migration is `go run ./api/cmd/merlon-import`
with the fixed CSV contract in ADR-0015. Run it once with `--dry-run` (the
default), inspect the JSON report, then use `--apply` after migrations are
current. `--apply` also requires `--actor <operator-or-service-id>` so both
successful and failed runs have an accountable, durable audit record. The command writes only customers, accounts, account links, and
transactions; alerts, cases, screening results, score history, and audit
artifacts are derived by Merlon after the import. The REST replay script in
[`scripts/migrate-initial-data.py`](https://github.com/ksuk/merlon/blob/main/scripts/migrate-initial-data.py)
remains available for small compatibility loads.

:::danger[Never write customer rows with SQL]

Direct-PII attributes (`full_name`, `address`, `date_of_birth`, `phone`,
`email`, `account_number`, `id_document_number`) are encrypted **by the
application** on write, in `api/internal/store/customer_pii.go`. A `COPY`,
`INSERT`, or `psql \copy` into the `customers` table stores that data in
plaintext in columns the read path will try to decrypt. The result is both a
data-protection failure and a database that Merlon cannot read back.

Every customer record must be written through the API.

:::

## Before you start

1. Apply migrations and confirm the API is healthy — see the
   [Deployment Runbook](deployment.md).
2. Confirm encryption keys are configured. Loading customers before
   encryption is configured writes unencrypted PII that a later key rollout
   will not retroactively protect.
3. Take a backup of the empty database so a failed load can be reset cleanly
   rather than partially undone. See the
   [Backup and Restore Runbook](backup-restore.md).
4. Issue an API key with the permissions the load needs, and plan to revoke
   it when the migration finishes.

Run the whole sequence against a staging copy first. The load is not
transactional (see [Failure handling](#failure-handling)), so a rehearsal is
the only way to find data problems cheaply.

## Step 1: Load customers

`POST /api/v1/customers` creates one customer per request.

```json
{
  "external_id": "CIF-000123",
  "customer_type": "individual",
  "country_code": "JP",
  "product_types": ["deposit"],
  "attributes": {
    "full_name": "…",
    "date_of_birth": "1980-01-01",
    "occupation": "…"
  }
}
```

- `external_id` is required and is your system's key for the customer. It has
  a `UNIQUE` constraint (`migrations/001_init.sql`), which is what makes the
  load restartable — see [Failure handling](#failure-handling).
- `customer_type` must be `individual`, `corporate_domestic`, or
  `corporate_foreign`.
- **Merlon assigns its own `id`.** You cannot supply one. The response body
  contains the assigned `id`; you must keep the `external_id` → `id` mapping,
  because transactions are posted against Merlon's `id`, not your key.
- Direct-PII attribute keys are encrypted on write. Everything else in
  `attributes` (occupation, industry, nationality, PEP flags) stays in
  plaintext so it remains indexable for scoring.

See the [Adapter Guide](../adapter-guide.md) for mapping source-system fields
onto this shape.

## Step 2: Load transaction history

`POST /api/v1/transactions`, one request per transaction.

```json
{
  "external_id": "TXN-2026-0000001",
  "customer_id": "<the id Merlon returned in step 1>",
  "amount": 1500000,
  "currency": "JPY",
  "direction": "outbound",
  "channel": "atm",
  "executed_at": "2026-04-01T09:30:00Z",
  "counterparty_id": "CP-000987",
  "counterparty_country": "KP"
}
```

- `customer_id` is Merlon's internal ID. Resolve it from the mapping you kept
  in step 1; an unknown ID is rejected with `400`.
- `external_id` is required and `UNIQUE`, same as for customers.
- `amount` must be positive, and `direction` must be `inbound`, `outbound`,
  or `internal`.
- `executed_at` is the transaction time from your source system. Load it
  accurately: TM scenarios evaluate over windows anchored on this field, so a
  defaulted or ingest-time value silently invalidates the backfill in step 4.
- `counterparty_country` is optional to the API but load it whenever your
  source has it. The high-risk-country scenario compares exactly this field,
  so omitting it leaves that control unable to fire for any imported transfer
  — a gap that looks identical to "no high-risk transfers occurred".
- **`currency` must already be the deployment's TM base currency**
  (`MERLON_TM_BASE_CURRENCY`, default `JPY`). Merlon does not convert. The
  engine sums nominal amounts, so a foreign-currency row would be compared
  against base-currency thresholds; both the realtime path and the step 4
  backfill refuse such a customer and park them in `PENDING_REVIEW` instead of
  producing a wrong result. Normalize amounts in your export.

Decide deliberately how much history to load. Transaction monitoring
scenarios evaluate over lookback windows, so loading less history than your
longest scenario window means those scenarios cannot fire correctly for the
first period after go-live.

## Step 3: Backfill CDD scores

Customers created in step 1 are unscored. Score them before monitoring, since
TM thresholds are derived from the CDD risk tier (ADR-0004, Score-Driven
Architecture).

`POST /api/v1/batch/score` takes **at most 1000 customer IDs per request**
(`maxBatchCustomers` in `api/internal/server/batch.go`); a longer list is
rejected with `400`. Chunk the ID list accordingly.

```json
{ "customer_ids": ["…", "…"] }
```

The response reports `total`, `succeeded`, `failed`, and a per-customer
`results` array. Treat a non-zero `failed` count as a stop condition and
inspect the per-customer `error` values — do not proceed to monitoring with
partially scored customers.

Check `total` and `succeeded` too, not just `failed`. An ID the server does
not recognise is skipped before evaluation and never reaches `results`, so it
lowers `total` without incrementing `failed`. Require
`total == succeeded == len(chunk)` for every chunk.

## Step 4: Backfill transaction monitoring

`POST /api/v1/batch/monitor`, with the same 1000-ID-per-request limit, runs
the TM scenarios over the loaded history and produces alerts. It evaluates
each customer's full loaded history, not a recent slice of it.

**Run it twice, once per evaluation mode.** A scenario declares which passes
it belongs to, and the engine applies only the matching ones:

```json
{ "customer_ids": ["…", "…"], "mode": "realtime" }
{ "customer_ids": ["…", "…"], "mode": "batch" }
```

- `mode` is optional and defaults to `realtime`, which is what the endpoint
  has always done.
- A realtime pass never applies an `evaluation_mode: batch` scenario — among
  the shipped samples, dormant account reactivation and high-frequency small
  amounts. A batch pass never applies a realtime-only one, such as high-risk
  country transfer. **A single pass therefore leaves part of your rule set
  unapplied over the whole imported history, and reports success anyway.**
- Scenarios declaring `both` run in each pass; Merlon deduplicates the
  resulting alerts by (customer, scenario, aggregation window), so the second
  pass does not double-count.
- `mode: "batch"` skips `dormant` customers, matching the scheduled TM batch
  job (the data model §1.1.2: a dormant customer is evaluated only when a
  transaction occurs). `closed` customers are skipped in both modes.

The response adds `queued_for_review` to the fields above. It counts
customers the engine could not evaluate — engine unavailable, or a history
Merlon refuses to aggregate, such as mixed currencies — whose transactions
were parked in `PENDING_REVIEW`. Those customers were **not** monitored.
Treat a non-zero `queued_for_review` as a stop condition alongside `failed`.

Expect this step to generate a large alert backlog: you are evaluating years
of history in one pass, against thresholds that have not yet been tuned for
your portfolio. Plan for triage capacity, and consider running the
[backtest endpoints](../api/openapi.md) against candidate rule sets first to
size the alert volume before committing to it.

## Rate limiting and throughput

The API applies a global rate limit (`api/internal/server/ratelimit.go`) and
returns `429` with `X-RateLimit-Limit` and `X-RateLimit-Remaining` headers
when it is exceeded. Because steps 1 and 2 are one request per record, a
large portfolio takes a long time.

Plan for this rather than fighting it:

- Have your client back off and retry on `429` rather than treating it as a
  failure.
- Run the migration during a maintenance window, when the limiter budget is
  not shared with live traffic.
- If the load is still impractically slow, raise the limit for the duration
  of the migration and restore it afterwards. Record the change — a
  temporarily widened rate limit is a control change.

## Failure handling

**There is no global transaction.** Each record is committed by its own
request, so an interrupted load leaves a partially populated database. There
is no rollback command.

The `UNIQUE` constraint on `external_id` is what keeps a restart safe: a
re-posted record is rejected by the database rather than duplicated. But note
two rough edges, because they shape how your client must be written:

- A rejected duplicate surfaces as `500`, not `409` — the create handler maps
  every repository error to an internal error. A `500` during a rerun is
  therefore ambiguous on its own.
- There is no lookup by `external_id`. `GET /api/v1/customers` supports only
  cursor and offset pagination, so you cannot cheaply ask "was this record
  already loaded?"

The practical consequence: **your client must keep its own checkpoint** of
which `external_id`s it has successfully posted, along with the `id` Merlon
returned, and resume from that file rather than from the server's state. The
reference script writes such a checkpoint after every successful record and
skips already-recorded IDs on a rerun.

For a first migration, restoring the pre-migration backup and starting over
is usually cleaner than reconciling a partial load.

Record what you loaded: source export file names and checksums, record
counts per step, start and end timestamps, and the operator. The API writes
audit entries for the records themselves, but the provenance of the export
files is outside Merlon and has to be captured by you.

## Verification

Before declaring the migration complete:

1. Compare record counts against the source export. `GET /api/v1/dashboard`
   returns `total_customers` and a `customers_by_risk_tier` breakdown.
   **Caveat:** the dashboard counts at most 10,000 customers
   (`api/internal/server/dashboard.go`), so above that size it is a health
   signal, not a reconciliation. For a real count, page through
   `GET /api/v1/customers` with the cursor, or compare against the checkpoint
   file your loader wrote.
2. Confirm `customers_by_risk_tier` has an empty or zero `unscored` bucket.
   An unscored customer is not monitored at the correct threshold, so this is
   the check that step 3 actually completed.
3. Confirm direct PII was actually encrypted, **by inspecting the stored
   value, not an API read**:

   ```sql
   SELECT attributes->>'full_name' FROM customers LIMIT 5;
   ```

   An encrypted value is base64 (key version byte + nonce + AES-GCM
   ciphertext) and unreadable. A legible name means that row reached the
   database without going through the API, and its PII is stored in plaintext.

   Do not use an API read for this check. `decryptDirectPII`
   (`api/internal/store/customer_pii.go`) deliberately returns a value it
   cannot decrypt unchanged, so that enabling encryption does not break reads
   of pre-existing rows. The consequence is that plaintext written by a direct
   `INSERT` reads back through the API looking perfectly correct — the API
   cannot distinguish an encrypted write from a bypassed one.
4. Spot-check several customers end to end and confirm the non-PII attributes,
   product types, and country codes match the source export.
5. Verify transaction timestamps survived the load: pick the oldest and
   newest records and confirm `executed_at` matches the source export rather
   than the load time.
6. Review the alert backlog volume from step 4 against your triage capacity
   before opening the system to analysts.
7. Revoke the migration API key.
