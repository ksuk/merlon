#!/usr/bin/env python3
"""Tests for the reference initial-data migration script.

The script is not a product feature, but it is the documented path for loading
a production customer master into Merlon, so the parts that can silently
produce a wrong migration are worth pinning:

  * the backfill accounting, which decides whether a load may be declared
    complete (Fail-Alert),
  * the payload builders, whose omissions disarm TM controls without any
    visible error,
  * the checkpoint's dry-run behaviour, which decides whether a rehearsal
    poisons the real load.

Standard library only, matching the script itself: `python3 -m unittest` runs
them with no install step.
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import os
import tempfile
import unittest

# The script is named with hyphens (it is run, not imported), so it cannot be
# imported by name.
_SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "migrate-initial-data.py")
_spec = importlib.util.spec_from_file_location("migrate_initial_data", _SCRIPT)
mig = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mig)


class CheckChunkResultTest(unittest.TestCase):
    """The guard that decides whether a backfill chunk may count as done."""

    CHUNK = ["c1", "c2"]

    def ok(self, **overrides) -> dict:
        result = {"total": 2, "succeeded": 2, "failed": 0, "queued_for_review": 0,
                  "results": [{"customer_id": "c1"}, {"customer_id": "c2"}]}
        result.update(overrides)
        return result

    def test_fully_evaluated_chunk_passes(self):
        mig.check_chunk_result("scoring", self.CHUNK, self.ok())

    def test_null_results_array_does_not_crash(self):
        # The API serialises an empty result array as null rather than [], so a
        # get() default never applies. A chunk of unknown ids hits exactly this.
        mig.check_chunk_result("scoring", self.CHUNK,
                               self.ok(total=2, succeeded=2, results=None))

    def test_failure_stops_the_load(self):
        with self.assertRaises(mig.MigrationError):
            mig.check_chunk_result("scoring", self.CHUNK, self.ok(failed=1, succeeded=1))

    def test_pending_review_is_not_success(self):
        # queued_for_review counts customers the engine could not evaluate.
        # They are parked, not processed, and are counted separately from
        # failed -- so checking failed alone would read this as a clean pass.
        with self.assertRaises(mig.MigrationError) as ctx:
            mig.check_chunk_result("monitoring", self.CHUNK,
                                   self.ok(succeeded=1, queued_for_review=1))
        self.assertIn("PENDING_REVIEW", str(ctx.exception))

    def test_unknown_id_dropped_by_the_server_stops_the_load(self):
        # An id the server does not recognise is skipped before evaluation and
        # never reaches results, lowering total without incrementing failed.
        with self.assertRaises(mig.MigrationError) as ctx:
            mig.check_chunk_result("scoring", self.CHUNK,
                                   self.ok(total=1, succeeded=1, results=None))
        self.assertIn("unknown ids", str(ctx.exception))

    def test_short_success_count_stops_the_load(self):
        with self.assertRaises(mig.MigrationError):
            mig.check_chunk_result("scoring", self.CHUNK, self.ok(succeeded=1))


class TransactionPayloadTest(unittest.TestCase):
    BASE_ROW = {
        "external_id": "TXN-1", "amount": "1500000", "currency": "JPY",
        "direction": "outbound", "channel": "wire",
        "executed_at": "2026-04-01T09:30:00Z",
    }

    def test_counterparty_country_is_carried_through(self):
        # The shipped high-risk-country scenario compares this field; dropping
        # it leaves that control unable to fire for any imported transfer.
        row = {**self.BASE_ROW, "counterparty_id": "CP-1", "counterparty_country": "KP"}
        payload = mig.transaction_payload(row, "cust-1", "JPY")
        self.assertEqual(payload["counterparty_country"], "KP")
        self.assertEqual(payload["counterparty_id"], "CP-1")

    def test_absent_counterparty_fields_are_omitted_not_blanked(self):
        payload = mig.transaction_payload({**self.BASE_ROW, "counterparty_country": ""},
                                          "cust-1", "JPY")
        self.assertNotIn("counterparty_country", payload)
        self.assertNotIn("counterparty_id", payload)

    def test_currency_defaults_to_base_and_is_upper_cased(self):
        payload = mig.transaction_payload({**self.BASE_ROW, "currency": ""}, "cust-1", "JPY")
        self.assertEqual(payload["currency"], "JPY")
        payload = mig.transaction_payload({**self.BASE_ROW, "currency": "jpy"}, "cust-1", "JPY")
        self.assertEqual(payload["currency"], "JPY")

    def test_amount_is_numeric_and_customer_id_is_merlons(self):
        payload = mig.transaction_payload(self.BASE_ROW, "cust-1", "JPY")
        self.assertEqual(payload["amount"], 1500000.0)
        self.assertEqual(payload["customer_id"], "cust-1")


class CustomerPayloadTest(unittest.TestCase):
    def test_attr_columns_become_attributes_and_blanks_are_dropped(self):
        payload = mig.customer_payload({
            "external_id": "CIF-1", "customer_type": "individual", "country_code": "JP",
            "product_types": "deposit;remittance", "attr.full_name": "山田 太郎",
            "attr.occupation": "",
        })
        self.assertEqual(payload["attributes"], {"full_name": "山田 太郎"})
        self.assertEqual(payload["product_types"], ["deposit", "remittance"])


class CheckpointTest(unittest.TestCase):
    @staticmethod
    def load(path: str, **kwargs) -> "mig.Checkpoint":
        """Build a Checkpoint without its resume banner reaching test output."""
        with contextlib.redirect_stdout(io.StringIO()):
            return mig.Checkpoint(path, **kwargs)

    def test_dry_run_checkpoint_is_never_written(self):
        # A dry run that recorded its synthetic ids would make the subsequent
        # real load skip every record it "already loaded".
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "state.json")
            ckpt = self.load(path, read_only=True)
            ckpt.customers["CIF-1"] = "dry-run-CIF-1"
            ckpt.save()
            self.assertFalse(os.path.exists(path))

    def test_dry_run_still_reads_an_existing_checkpoint(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "state.json")
            with open(path, "w", encoding="utf-8") as fh:
                json.dump({"customers": {"CIF-1": "real-id"}, "transactions": ["TXN-1"]}, fh)
            ckpt = self.load(path, read_only=True)
            self.assertEqual(ckpt.customers, {"CIF-1": "real-id"})
            self.assertEqual(ckpt.transactions, {"TXN-1"})

    def test_normal_run_persists_and_reloads(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "state.json")
            ckpt = self.load(path)
            ckpt.customers["CIF-1"] = "real-id"
            ckpt.transactions.add("TXN-1")
            ckpt.save()
            self.assertEqual(self.load(path).customers, {"CIF-1": "real-id"})


class FakeClient:
    def __init__(self, config: dict | None = None):
        self.config = config if config is not None else {}

    def get(self, path: str) -> dict:
        assert path == "/api/v1/system/config-digests", path
        return self.config


class ResolveBaseCurrencyTest(unittest.TestCase):
    def test_reads_the_deployments_base_currency(self):
        self.assertEqual(
            mig.resolve_base_currency(FakeClient({"base_currency": "USD"}), ""), "USD")

    def test_override_wins_and_is_upper_cased(self):
        self.assertEqual(mig.resolve_base_currency(FakeClient(), "jpy"), "JPY")

    def test_missing_base_currency_stops_rather_than_guessing_jpy(self):
        with self.assertRaises(mig.MigrationError):
            mig.resolve_base_currency(FakeClient({}), "")


if __name__ == "__main__":
    unittest.main()
