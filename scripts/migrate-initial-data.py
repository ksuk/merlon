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
    channel, executed_at
    - customer_external_id is YOUR key; it is resolved to Merlon's customer
      id through the checkpoint written by the customer phase.
    - executed_at is an RFC 3339 timestamp and must be the real transaction
      time: TM scenarios evaluate over windows anchored on it.

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
    """

    def __init__(self, path: str) -> None:
        self.path = path
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
            return {"id": f"dry-run-{payload.get('external_id', 'x')}"}

        body = json.dumps(payload).encode("utf-8")
        backoff = INITIAL_BACKOFF_SECONDS

        for attempt in range(1, MAX_RETRIES + 1):
            req = urllib.request.Request(
                f"{self.base_url}{path}",
                data=body,
                method="POST",
                headers={
                    "Content-Type": "application/json",
                    "Authorization": f"Bearer {self.api_key}",
                },
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
                    f"POST {path} failed: HTTP {err.code}: {detail}"
                ) from err
            except urllib.error.URLError as err:
                raise MigrationError(f"POST {path} failed: {err.reason}") from err

        raise MigrationError(f"POST {path} still rate limited after {MAX_RETRIES} attempts")


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


def transaction_payload(row: dict, customer_id: str) -> dict:
    return {
        "external_id": row["external_id"],
        "customer_id": customer_id,
        "amount": float(row["amount"]),
        "currency": row.get("currency") or "JPY",
        "direction": row["direction"],
        "channel": row.get("channel") or "",
        "executed_at": row["executed_at"],
    }


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


def load_transactions(client: MerlonClient, path: str, ckpt: Checkpoint) -> None:
    print(f"\n== Loading transactions from {path}")
    loaded = skipped = 0
    with open(path, newline="", encoding="utf-8") as fh:
        for line_no, row in enumerate(csv.DictReader(fh), start=2):
            external_id = (row.get("external_id") or "").strip()
            if not external_id:
                raise MigrationError(f"{path}:{line_no}: external_id is required")
            if external_id in ckpt.transactions:
                skipped += 1
                continue

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
                    "/api/v1/transactions", transaction_payload(row, customer_id)
                )
            except MigrationError as err:
                raise MigrationError(f"{path}:{line_no}: {err}") from err
            ckpt.transactions.add(external_id)
            ckpt.save()
            loaded += 1
            if loaded % 500 == 0:
                print(f"  {loaded} loaded...")
    print(f"transactions: {loaded} loaded, {skipped} already present")


def backfill(client: MerlonClient, ckpt: Checkpoint) -> None:
    """Score every loaded customer, then run TM over the loaded history.

    Order matters: TM thresholds are derived from the CDD risk tier
    (ADR-0004, Score-Driven Architecture), so monitoring before scoring
    evaluates against the wrong thresholds.
    """
    customer_ids = sorted(ckpt.customers.values())
    if not customer_ids:
        raise MigrationError("checkpoint contains no customers to backfill")

    for label, path in (
        ("scoring", "/api/v1/batch/score"),
        ("monitoring", "/api/v1/batch/monitor"),
    ):
        print(f"\n== Backfill: {label} ({len(customer_ids)} customers)")
        failed_total = 0
        for start in range(0, len(customer_ids), MAX_BATCH_CUSTOMERS):
            chunk = customer_ids[start:start + MAX_BATCH_CUSTOMERS]
            result = client.post(path, {"customer_ids": chunk})
            failed = result.get("failed", 0)
            failed_total += failed
            print(f"  {start + len(chunk)}/{len(customer_ids)}"
                  f" succeeded={result.get('succeeded', 0)} failed={failed}")
            for item in result.get("results", []):
                if item.get("error"):
                    print(f"    {item.get('customer_id')}: {item['error']}")

        if failed_total:
            # Fail-Alert: a partial backfill leaves customers evaluated at the
            # wrong threshold, which is worse than a load that stops loudly.
            raise MigrationError(
                f"{label} backfill had {failed_total} failure(s); "
                "resolve them before continuing"
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
    parser.add_argument("--dry-run", action="store_true",
                        help="print requests instead of sending them")
    args = parser.parse_args()

    if not (args.customers or args.transactions or args.backfill):
        parser.error("nothing to do: pass --customers, --transactions, and/or --backfill")
    if not args.api_key and not args.dry_run:
        parser.error("no API key: pass --api-key or set MERLON_API_KEY")

    client = MerlonClient(args.api_url, args.api_key, dry_run=args.dry_run)
    ckpt = Checkpoint(args.checkpoint)

    try:
        if args.customers:
            load_customers(client, args.customers, ckpt)
        if args.transactions:
            load_transactions(client, args.transactions, ckpt)
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
