#!/usr/bin/env python3
"""Reference initial-data migration for Merlon.

Loads an existing customer master and transaction history from CSV files into
a fresh Merlon installation, following docs/operations/initial-migration.md:

  1. POST /api/v1/customers        (one request per customer)
  2. POST /api/v1/transactions     (one request per transaction)
  3. POST /api/v1/batch/score      (chunks of <= 1000 customer IDs)
  4. POST /api/v1/batch/monitor    (chunks of <= 1000 customer IDs)

This is a reference implementation meant to be read and adapted, not a
supported product feature. It deliberately uses only the Python standard
library so it runs anywhere without an install step.

Two properties matter more than speed, and are the reason this is not a
twenty-line curl loop:

  * Restartability. The load is not transactional -- every record is
    committed by its own request. The API offers no lookup by external_id and
    reports a duplicate as 500 rather than 409, so the client cannot ask the
    server what it already has. This script therefore keeps its own
    checkpoint file, appending each external_id together with the id Merlon
    assigned, and skips those entries on a rerun.

  * Rate-limit tolerance. The API rate-limits globally and answers 429. A
    migration that treats 429 as a failure will die partway through a large
    portfolio; this one backs off and retries.

Customer CSV columns:
    external_id, customer_type, country_code, product_types, attr.*
    - product_types is semicolon-separated ("deposit;remittance")
    - any attr.<name> column becomes attributes.<name>, e.g. attr.full_name.
      Direct-PII attributes are encrypted by the API on write; never load
      them by any other route.

Transaction CSV columns:
    external_id, customer_external_id, amount, currency, direction,
    channel, executed_at, counterparty_id, counterparty_country
    - customer_external_id is YOUR key; it is resolved to Merlon's customer
      id through the checkpoint written by the customer phase.
    - executed_at is an RFC 3339 timestamp and must be the real transaction
      time: TM scenarios evaluate over windows anchored on it.
    - counterparty_country is the ISO country code of the other side. Drop it
      and the shipped high-risk-country scenario
      (content/_sample/tm_scenarios/high_risk_country_transfer.yaml) compares
      against an empty field and can never fire for imported transfers.
    - currency must already be the deployment's TM base currency
      (MERLON_TM_BASE_CURRENCY, default JPY). The engine sums nominal amounts,
      so a mixed-currency history is compared against base-currency thresholds
      and produces wrong results; this script refuses such rows up front.

Usage:
    export MERLON_API_KEY=...
    python3 scripts/migrate-initial-data.py \
        --api-url http://localhost:8080 \
        --customers customers.csv \
        --transactions transactions.csv \
        --checkpoint migration-state.json

    # then, once counts are verified:
    python3 scripts/migrate-initial-data.py --api-url ... \
        --checkpoint migration-state.json --backfill

--dry-run prints the requests it would send and never writes the checkpoint.
A dry run that recorded its synthetic ids would make the subsequent real load
skip every record it "already loaded" and backfill against ids that do not
exist -- reporting a clean migration over an empty database.
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import sys
import time
import urllib.error
import urllib.request

# The API rejects a batch request carrying more than this many customer ids
# (maxBatchCustomers in api/internal/server/batch.go).
MAX_BATCH_CUSTOMERS = 1000

# 429 backoff. The API sets X-RateLimit-* headers and may set Retry-After; we
# honour Retry-After when present and fall back to exponential backoff.
MAX_RETRIES = 8
INITIAL_BACKOFF_SECONDS = 1.0


class MigrationError(Exception):
    """A failure that should stop the load rather than be retried."""


# ---------------------------------------------------------------------------
# Checkpoint
# ---------------------------------------------------------------------------


class Checkpoint:
    """Records what has been loaded, so a rerun resumes instead of retrying.

    Held in memory and flushed after every successful write. Flushing that
    often is deliberate: the whole point is to survive a process that dies
    mid-load, and a buffered checkpoint would lose exactly the records it
    exists to remember.

    read_only makes save() a no-op. A dry run still reads an existing
    checkpoint -- so it reports honestly which records a rerun would skip --
    but must never write back the synthetic ids it invents, which would leave
    the real load with nothing to do and nothing real to backfill.
    """

    def __init__(self, path: str, read_only: bool = False) -> None:
        self.path = path
        self.read_only = read_only
        self.customers: dict[str, str] = {}  # external_id -> merlon id
        self.transactions: set[str] = set()  # external_id
        self._load()

    def _load(self) -> None:
        if not os.path.exists(self.path):
            return
        with open(self.path, encoding="utf-8") as fh:
            data = json.load(fh)
        self.customers = data.get("customers", {})
        self.transactions = set(data.get("transactions", []))
        print(
            f"resuming from {self.path}: "
            f"{len(self.customers)} customers, "
            f"{len(self.transactions)} transactions already loaded"
        )

    def save(self) -> None:
        if self.read_only:
            return
        tmp = f"{self.path}.tmp"
        with open(tmp, "w", encoding="utf-8") as fh:
            json.dump(
                {
                    "customers": self.customers,
                    "transactions": sorted(self.transactions),
                },
                fh,
                indent=2,
            )
        os.replace(tmp, self.path)  # atomic: never leave a half-written file


# ---------------------------------------------------------------------------
# HTTP
# ---------------------------------------------------------------------------


class MerlonClient:
    def __init__(self, base_url: str, api_key: str, dry_run: bool = False) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.dry_run = dry_run

    def post(self, path: str, payload: dict) -> dict:
        if self.dry_run:
            print(f"  DRY RUN POST {path} {json.dumps(payload, ensure_ascii=False)}")
            # A synthetic id, never persisted: Checkpoint is read-only under
            # --dry-run precisely so this value cannot leak into a real load.
            return {"id": f"dry-run-{payload.get('external_id', 'x')}"}
        return self._send("POST", path, json.dumps(payload).encode("utf-8"))

    def get(self, path: str) -> dict:
        return self._send("GET", path, None)

    def _send(self, method: str, path: str, body: bytes | None) -> dict:
        backoff = INITIAL_BACKOFF_SECONDS
        headers = {"Authorization": f"Bearer {self.api_key}"}
        if body is not None:
            headers["Content-Type"] = "application/json"

        for attempt in range(1, MAX_RETRIES + 1):
            req = urllib.request.Request(
                f"{self.base_url}{path}",
                data=body,
                method=method,
                headers=headers,
            )
            try:
                with urllib.request.urlopen(req) as resp:
                    return json.loads(resp.read() or b"{}")
            except urllib.error.HTTPError as err:
                detail = err.read().decode("utf-8", "replace")
                if err.code == 429 and attempt < MAX_RETRIES:
                    wait = float(err.headers.get("Retry-After") or backoff)
                    print(f"  rate limited, retrying in {wait:.1f}s "
                          f"(attempt {attempt}/{MAX_RETRIES})")
                    time.sleep(wait)
                    backoff *= 2
                    continue
                raise MigrationError(
                    f"{method} {path} failed: HTTP {err.code}: {detail}"
                ) from err
            except urllib.error.URLError as err:
                raise MigrationError(f"{method} {path} failed: {err.reason}") from err

        raise MigrationError(f"{method} {path} still rate limited after {MAX_RETRIES} attempts")


# ---------------------------------------------------------------------------
# Row -> payload
# ---------------------------------------------------------------------------


def customer_payload(row: dict) -> dict:
    attributes = {
        key[len("attr."):]: value
        for key, value in row.items()
        if key.startswith("attr.") and value not in (None, "")
    }
    product_types = [
        p.strip() for p in (row.get("product_types") or "").split(";") if p.strip()
    ]
    return {
        "external_id": row["external_id"],
        "customer_type": row["customer_type"],
        "country_code": row.get("country_code") or "",
        "product_types": product_types,
        "attributes": attributes,
    }


def transaction_payload(row: dict, customer_id: str, base_currency: str) -> dict:
    payload = {
        "external_id": row["external_id"],
        "customer_id": customer_id,
        "amount": float(row["amount"]),
        "currency": (row.get("currency") or base_currency).upper(),
        "direction": row["direction"],
        "channel": row.get("channel") or "",
        "executed_at": row["executed_at"],
    }
    # Optional, but load them when the source has them: the shipped
    # high-risk-country scenario compares counterparty_country, so omitting it
    # silently disarms that control for every imported transfer.
    for key in ("counterparty_id", "counterparty_country"):
        value = (row.get(key) or "").strip()
        if value:
            payload[key] = value
    return payload


def resolve_base_currency(client: MerlonClient, override: str) -> str:
    """Return the TM base currency this deployment aggregates in.

    The engine sums nominal amounts and compares them against thresholds
    expressed in this currency (MERLON_TM_BASE_CURRENCY). Loading a row in
    anything else produces a detection result that is quietly wrong: the
    realtime path fail-alerts it into PENDING_REVIEW, and the batch pass
    refuses to evaluate the customer at all. Normalize before loading.
    """
    if override:
        return override.upper()
    config = client.get("/api/v1/system/config-digests")
    base = (config.get("base_currency") or "").upper()
    if not base:
        raise MigrationError(
            "could not determine the TM base currency from "
            "/api/v1/system/config-digests; pass --base-currency explicitly"
        )
    return base


# ---------------------------------------------------------------------------
# Phases
# ---------------------------------------------------------------------------


def load_customers(client: MerlonClient, path: str, ckpt: Checkpoint) -> None:
    print(f"\n== Loading customers from {path}")
    loaded = skipped = 0
    with open(path, newline="", encoding="utf-8") as fh:
        for line_no, row in enumerate(csv.DictReader(fh), start=2):
            external_id = (row.get("external_id") or "").strip()
            if not external_id:
                raise MigrationError(f"{path}:{line_no}: external_id is required")
            if external_id in ckpt.customers:
                skipped += 1
                continue
            try:
                created = client.post("/api/v1/customers", customer_payload(row))
            except MigrationError as err:
                raise MigrationError(f"{path}:{line_no}: {err}") from err
            ckpt.customers[external_id] = created["id"]
            ckpt.save()
            loaded += 1
            if loaded % 100 == 0:
                print(f"  {loaded} loaded...")
    print(f"customers: {loaded} loaded, {skipped} already present")


def load_transactions(
    client: MerlonClient, path: str, ckpt: Checkpoint, base_currency: str
) -> None:
    print(f"\n== Loading transactions from {path} (base currency {base_currency})")
    loaded = skipped = 0
    with open(path, newline="", encoding="utf-8") as fh:
        for line_no, row in enumerate(csv.DictReader(fh), start=2):
            external_id = (row.get("external_id") or "").strip()
            if not external_id:
                raise MigrationError(f"{path}:{line_no}: external_id is required")
            if external_id in ckpt.transactions:
                skipped += 1
                continue

            currency = (row.get("currency") or base_currency).strip().upper()
            if currency != base_currency:
                # Stop rather than load: the row would be accepted and then
                # aggregated at its nominal amount against base-currency
                # thresholds. Converting it here would invent an exchange rate
                # this system has no business choosing.
                raise MigrationError(
                    f"{path}:{line_no}: currency {currency} is not the TM base "
                    f"currency {base_currency}; normalize amounts before loading"
                )

            customer_key = (row.get("customer_external_id") or "").strip()
            customer_id = ckpt.customers.get(customer_key)
            if not customer_id:
                # Fail rather than skip: a transaction whose customer is
                # missing would silently narrow the monitoring backfill.
                raise MigrationError(
                    f"{path}:{line_no}: customer_external_id {customer_key!r} was "
                    "not loaded; load the customer master first"
                )

            try:
                client.post(
                    "/api/v1/transactions",
                    transaction_payload(row, customer_id, base_currency),
                )
            except MigrationError as err:
                raise MigrationError(f"{path}:{line_no}: {err}") from err
            ckpt.transactions.add(external_id)
            ckpt.save()
            loaded += 1
            if loaded % 500 == 0:
                print(f"  {loaded} loaded...")
    print(f"transactions: {loaded} loaded, {skipped} already present")


def check_chunk_result(label: str, chunk: list[str], result: dict) -> None:
    """Require that every customer in the chunk was actually evaluated.

    Checking `failed` alone does not prove that. Two ways a chunk can come
    back clean without the work having happened:

      * `queued_for_review` counts customers the engine could not evaluate
        (engine down, or a snapshot the API refuses to aggregate). Those
        records are parked in PENDING_REVIEW, not processed -- they are
        counted separately from `failed` and must not read as success.
      * an id the server does not recognise is skipped before evaluation and
        never reaches the results array, so it lowers `total` without
        incrementing `failed`.

    Fail-Alert: a partial backfill leaves customers evaluated at the wrong
    threshold, or not evaluated at all, which is worse than a load that stops
    loudly.
    """
    # `or []` rather than a get() default: the API serialises an empty result
    # array as null, so the key is present and the default never applies.
    for item in result.get("results") or []:
        if item.get("error"):
            print(f"    {item.get('customer_id')}: {item['error']}")

    failed = result.get("failed", 0)
    queued = result.get("queued_for_review", 0)
    total = result.get("total", 0)
    succeeded = result.get("succeeded", 0)

    if failed:
        raise MigrationError(
            f"{label} backfill had {failed} failure(s); "
            "resolve them before continuing"
        )
    if queued:
        raise MigrationError(
            f"{label} backfill left {queued} customer(s) in PENDING_REVIEW "
            "instead of evaluating them; check the API logs (engine "
            "availability, non-base-currency history) before continuing"
        )
    if total != len(chunk):
        raise MigrationError(
            f"{label} backfill reported total={total} for a chunk of "
            f"{len(chunk)} customer id(s); the server silently dropped "
            "unknown ids -- the checkpoint does not match the database"
        )
    if succeeded != len(chunk):
        raise MigrationError(
            f"{label} backfill reported succeeded={succeeded} of "
            f"{len(chunk)}; every customer in the chunk must be evaluated"
        )


def run_backfill_pass(
    client: MerlonClient, label: str, path: str, payload: dict, customer_ids: list[str]
) -> None:
    print(f"\n== Backfill: {label} ({len(customer_ids)} customers)")
    for start in range(0, len(customer_ids), MAX_BATCH_CUSTOMERS):
        chunk = customer_ids[start:start + MAX_BATCH_CUSTOMERS]
        result = client.post(path, {**payload, "customer_ids": chunk})
        print(f"  {start + len(chunk)}/{len(customer_ids)}"
              f" succeeded={result.get('succeeded', 0)}"
              f" failed={result.get('failed', 0)}"
              f" queued_for_review={result.get('queued_for_review', 0)}")
        check_chunk_result(label, chunk, result)


def backfill(client: MerlonClient, ckpt: Checkpoint) -> None:
    """Score every loaded customer, then run TM over the loaded history.

    Order matters in two places.

    Scoring comes first: TM thresholds are derived from the CDD risk tier
    (ADR-0004, Score-Driven Architecture), so monitoring before scoring
    evaluates against the wrong thresholds.

    Monitoring then runs twice, once per evaluation mode. A scenario declares
    which passes it belongs to, and the engine filters accordingly: a realtime
    pass never applies an `evaluation_mode: batch` scenario (dormant account
    reactivation, high-frequency small amounts), and a batch pass never
    applies a realtime-only one (high-risk country transfer). One pass leaves
    half the rule set unapplied over the whole imported history. Scenarios
    declaring `both` run in each pass; the API deduplicates the resulting
    alerts by (customer, scenario, aggregation window).
    """
    customer_ids = sorted(ckpt.customers.values())
    if not customer_ids:
        raise MigrationError("checkpoint contains no customers to backfill")

    run_backfill_pass(client, "scoring", "/api/v1/batch/score", {}, customer_ids)
    for mode in ("realtime", "batch"):
        run_backfill_pass(
            client, f"monitoring ({mode})", "/api/v1/batch/monitor",
            {"mode": mode}, customer_ids,
        )


# ---------------------------------------------------------------------------


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Reference initial-data migration for Merlon.",
        epilog="See docs/operations/initial-migration.md for the full runbook.",
    )
    parser.add_argument("--api-url", default="http://localhost:8080",
                        help="Merlon API base URL (default: %(default)s)")
    parser.add_argument("--api-key", default=os.environ.get("MERLON_API_KEY", ""),
                        help="API key; defaults to $MERLON_API_KEY")
    parser.add_argument("--customers", help="customer master CSV")
    parser.add_argument("--transactions", help="transaction history CSV")
    parser.add_argument("--checkpoint", default="migration-state.json",
                        help="resume/checkpoint file (default: %(default)s)")
    parser.add_argument("--backfill", action="store_true",
                        help="run CDD scoring and TM monitoring over loaded customers")
    parser.add_argument("--base-currency", default="",
                        help="TM base currency; read from the API when omitted")
    parser.add_argument("--dry-run", action="store_true",
                        help="print requests instead of sending them; never "
                             "writes the checkpoint")
    args = parser.parse_args()

    if not (args.customers or args.transactions or args.backfill):
        parser.error("nothing to do: pass --customers, --transactions, and/or --backfill")
    if not args.api_key and not args.dry_run:
        parser.error("no API key: pass --api-key or set MERLON_API_KEY")
    if args.dry_run and args.transactions and not args.base_currency:
        parser.error("--dry-run with --transactions needs --base-currency: "
                     "the base currency cannot be read from the API offline")

    client = MerlonClient(args.api_url, args.api_key, dry_run=args.dry_run)
    ckpt = Checkpoint(args.checkpoint, read_only=args.dry_run)

    try:
        if args.customers:
            load_customers(client, args.customers, ckpt)
        if args.transactions:
            base_currency = resolve_base_currency(client, args.base_currency)
            load_transactions(client, args.transactions, ckpt, base_currency)
        if args.backfill:
            backfill(client, ckpt)
    except MigrationError as err:
        print(f"\nERROR: {err}", file=sys.stderr)
        print(f"Progress is saved in {args.checkpoint}; rerun to resume.",
              file=sys.stderr)
        return 1

    print("\nDone. Verify the load per docs/operations/initial-migration.md "
          "before opening the system to analysts.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
