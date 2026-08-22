"""Tests for the ruleset baseline canonicalizer and guard.

The case these exist for is the one that already happened: the drift check ran
under a token that could not see `bypass_actors`, the field was omitted from
the response rather than returned empty, and the job reported the omission as
a difference from the baseline. An omitted key and an empty list are the same
thing to anything that compares values, so the distinction has to be asserted
somewhere that a change cannot quietly erase.

Assertions are on exit codes, never on message text. The messages will be
edited -- they are written for whoever reads a failing run at 00:17 on a
Monday -- and a test that pins the prose would turn every improvement to them
into a test failure.
"""

from __future__ import annotations

import json
import pathlib
import subprocess
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("ruleset-baseline.sh")
BASELINE_DIR = pathlib.Path(__file__).resolve().parents[1] / ".github" / "rulesets"

OK = 0
MALFORMED = 1
USAGE = 2
DEGRADED = 3
BYPASSABLE = 4


def raw_response(**overrides: object) -> dict[str, object]:
    """A ruleset as the API returns it to an Administration-scoped caller."""
    response: dict[str, object] = {
        "id": 1,
        "name": "main-release-governance",
        "target": "branch",
        "source_type": "Repository",
        "source": "ksuk/merlon",
        "enforcement": "active",
        "bypass_actors": [],
        "conditions": {"ref_name": {"include": ["refs/heads/main"], "exclude": []}},
        "rules": [{"type": "deletion"}, {"type": "non_fast_forward"}],
        # Per-request and per-viewer fields the canonical form strips.
        "current_user_can_bypass": "never",
        "_links": {"self": {"href": "https://api.github.com/..."}},
        "node_id": "RRS_abc",
        "created_at": "2026-07-25T00:00:00Z",
        "updated_at": "2026-08-16T00:00:00Z",
    }
    response.update(overrides)
    return response


class CanonicalizeTests(unittest.TestCase):
    def canonicalize(self, payload: object) -> subprocess.CompletedProcess[str]:
        text = payload if isinstance(payload, str) else json.dumps(payload)
        return subprocess.run(
            ["bash", str(SCRIPT), "--canonicalize"],
            input=text,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_admin_response_is_accepted_and_stripped(self) -> None:
        result = self.canonicalize(raw_response())
        self.assertEqual(result.returncode, OK)
        emitted = json.loads(result.stdout)
        for kept in ("bypass_actors", "conditions", "enforcement", "name", "rules", "target"):
            self.assertIn(kept, emitted)
        for stripped in ("_links", "node_id", "created_at", "updated_at", "current_user_can_bypass"):
            self.assertNotIn(stripped, emitted)

    # The defect this whole change exists for. A token without Administration
    # (read) receives no bypass_actors key at all.
    def test_absent_bypass_actors_is_a_token_failure_not_a_diff(self) -> None:
        payload = raw_response()
        del payload["bypass_actors"]
        self.assertEqual(self.canonicalize(payload).returncode, DEGRADED)

    # The distinction the guard turns on: empty is a real answer, absent is not.
    def test_empty_bypass_actors_is_accepted(self) -> None:
        self.assertEqual(self.canonicalize(raw_response(bypass_actors=[])).returncode, OK)

    def test_populated_bypass_actors_is_accepted_and_preserved(self) -> None:
        actors = [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}]
        result = self.canonicalize(raw_response(bypass_actors=actors))
        # Not this guard's job to reject it -- the committed diff is what makes
        # it visible, and that only works if the value survives the export.
        self.assertEqual(result.returncode, OK)
        self.assertEqual(json.loads(result.stdout)["bypass_actors"], actors)

    def test_every_decision_bearing_key_is_required(self) -> None:
        for key in ("conditions", "current_user_can_bypass", "enforcement", "name", "rules", "target"):
            with self.subTest(missing=key):
                payload = raw_response()
                del payload[key]
                self.assertEqual(self.canonicalize(payload).returncode, DEGRADED)

    def test_bypassable_viewer_is_a_finding(self) -> None:
        self.assertEqual(
            self.canonicalize(raw_response(current_user_can_bypass="always")).returncode,
            BYPASSABLE,
        )

    # Ordering guarantee: a degraded token must always report the token
    # problem, never a confusing verdict about the Actions app's viewpoint.
    def test_missing_key_outranks_bypassability(self) -> None:
        payload = raw_response(current_user_can_bypass="always")
        del payload["bypass_actors"]
        self.assertEqual(self.canonicalize(payload).returncode, DEGRADED)

    def test_malformed_input_is_rejected(self) -> None:
        self.assertEqual(self.canonicalize("not json").returncode, MALFORMED)
        self.assertEqual(self.canonicalize("").returncode, MALFORMED)
        self.assertEqual(self.canonicalize([]).returncode, MALFORMED)


class ComparableModeTests(unittest.TestCase):
    """The mode the weekly drift job runs in.

    It cannot see `bypass_actors` at all, so it compares a rendering without
    that field on both sides. The risk in doing so is that "the caller could
    not see it" quietly becomes "it is empty" -- which is the #115 defect
    wearing a different hat. These tests pin the boundary: comparable mode
    relaxes exactly one field and nothing else, and it never leaks the field
    into its output where only one side could populate it.
    """

    def run_mode(self, payload: object, *flags: str) -> subprocess.CompletedProcess[str]:
        text = payload if isinstance(payload, str) else json.dumps(payload)
        return subprocess.run(
            ["bash", str(SCRIPT), "--canonicalize", *flags],
            input=text,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_absent_bypass_actors_is_accepted(self) -> None:
        payload = raw_response()
        del payload["bypass_actors"]
        self.assertEqual(self.run_mode(payload, "--comparable").returncode, OK)

    def test_strict_still_rejects_what_comparable_accepts(self) -> None:
        payload = raw_response()
        del payload["bypass_actors"]
        self.assertEqual(self.run_mode(payload).returncode, DEGRADED)

    # Comparable mode relaxes ONE field. Anything else missing still means the
    # response cannot be trusted, and narrowing the comparison further would be
    # the same failure at a smaller scale.
    def test_every_other_key_is_still_required(self) -> None:
        for key in ("conditions", "enforcement", "name", "rules", "target"):
            with self.subTest(missing=key):
                payload = raw_response()
                del payload[key]
                self.assertEqual(self.run_mode(payload, "--comparable").returncode, DEGRADED)

    # If the field survived into the output, one side of the weekly comparison
    # could populate it and the other never could -- a permanent diff.
    def test_output_never_carries_bypass_actors(self) -> None:
        for payload in (raw_response(), raw_response(bypass_actors=[{"actor_id": 5}])):
            result = self.run_mode(payload, "--comparable")
            self.assertEqual(result.returncode, OK)
            self.assertNotIn("bypass_actors", json.loads(result.stdout))

    def test_bypassable_viewer_is_still_a_finding(self) -> None:
        payload = raw_response(current_user_can_bypass="always")
        self.assertEqual(self.run_mode(payload, "--comparable").returncode, BYPASSABLE)

    # The same absent-versus-empty trap, one field over. A caller that cannot
    # read administration fields cannot answer the bypass question either, so
    # comparable mode must not accept the field's absence as "never" -- it
    # asserts nothing instead, and the workflow says so.
    def test_absent_bypass_field_is_not_read_as_never(self) -> None:
        payload = raw_response()
        del payload["current_user_can_bypass"]
        # Strict: the field has to be observed to be asserted.
        self.assertEqual(self.run_mode(payload).returncode, DEGRADED)
        # Comparable: accepted, but nothing is claimed about bypassing.
        self.assertEqual(self.run_mode(payload, "--comparable").returncode, OK)

    def test_flag_order_does_not_matter(self) -> None:
        payload = raw_response()
        del payload["bypass_actors"]
        a = subprocess.run(
            ["bash", str(SCRIPT), "--canonicalize", "--comparable"],
            input=json.dumps(payload), capture_output=True, text=True, check=False,
        )
        b = subprocess.run(
            ["bash", str(SCRIPT), "--comparable", "--canonicalize"],
            input=json.dumps(payload), capture_output=True, text=True, check=False,
        )
        self.assertEqual(a.returncode, OK)
        self.assertEqual(b.returncode, OK)
        self.assertEqual(a.stdout, b.stdout)

    # The load-bearing property of the whole design: a degraded live export and
    # a complete committed baseline render identically, so the weekly job can be
    # green without anyone deleting bypass_actors from the baseline to get there.
    def test_degraded_live_and_complete_baseline_render_identically(self) -> None:
        degraded = raw_response()
        del degraded["bypass_actors"]
        complete = raw_response()
        a = self.run_mode(degraded, "--comparable")
        b = self.run_mode(complete, "--comparable")
        self.assertEqual(a.returncode, OK)
        self.assertEqual(b.returncode, OK)
        self.assertEqual(a.stdout, b.stdout)

    # A committed baseline must never be audited in the relaxed mode: that is
    # exactly how a degraded baseline would slip past the guard meant to catch it.
    def test_check_refuses_comparable(self) -> None:
        files = sorted(BASELINE_DIR.glob("*.json"))
        result = subprocess.run(
            ["bash", str(SCRIPT), "--check", "--comparable", *map(str, files)],
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(result.returncode, USAGE)


class CheckTests(unittest.TestCase):
    def check(self, *paths: pathlib.Path | str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(SCRIPT), "--check", *map(str, paths)],
            capture_output=True,
            text=True,
            check=False,
        )

    def canonical_file(self, tmp: str, payload: object) -> pathlib.Path:
        emitted = subprocess.run(
            ["bash", str(SCRIPT), "--canonicalize"],
            input=json.dumps(payload),
            capture_output=True,
            text=True,
            check=True,
        ).stdout
        path = pathlib.Path(tmp) / "ruleset.json"
        path.write_text(emitted, encoding="utf-8")
        return path

    # The guard must pass against what is committed today, or the pull request
    # that introduces it reds itself.
    # The baseline schema and the response schema are different objects. A
    # committed baseline has current_user_can_bypass stripped by design, so
    # requiring it there would fail every correctly produced baseline.
    def test_baseline_schema_does_not_demand_the_per_viewer_field(self) -> None:
        for f in sorted(BASELINE_DIR.glob("*.json")):
            with self.subTest(baseline=f.name):
                self.assertNotIn(
                    "current_user_can_bypass", json.loads(f.read_text(encoding="utf-8"))
                )
        self.assertEqual(self.check(*sorted(BASELINE_DIR.glob("*.json"))).returncode, OK)

    def test_committed_baselines_pass(self) -> None:
        files = sorted(BASELINE_DIR.glob("*.json"))
        self.assertTrue(files, "expected committed ruleset baselines to exist")
        self.assertEqual(self.check(*files).returncode, OK)

    # A glob that matches nothing must not pass by checking nothing.
    def test_zero_files_is_a_failure(self) -> None:
        self.assertEqual(self.check().returncode, MALFORMED)

    def test_baseline_stripped_of_bypass_actors_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = self.canonical_file(tmp, raw_response())
            payload = json.loads(path.read_text(encoding="utf-8"))
            del payload["bypass_actors"]
            path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
            self.assertEqual(self.check(path).returncode, MALFORMED)

    def test_baseline_carrying_a_per_viewer_field_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = self.canonical_file(tmp, raw_response())
            payload = json.loads(path.read_text(encoding="utf-8"))
            payload["current_user_can_bypass"] = "never"
            path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
            self.assertEqual(self.check(path).returncode, MALFORMED)

    # Hand-editing is what the idempotence assertion is for: the file stops
    # being what the drift export would produce, so the comparison stops
    # meaning what it claims to mean.
    def test_non_canonical_formatting_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = self.canonical_file(tmp, raw_response())
            path.write_text(json.dumps(json.loads(path.read_text(encoding="utf-8"))), encoding="utf-8")
            self.assertEqual(self.check(path).returncode, MALFORMED)

    def test_missing_and_malformed_files_are_rejected(self) -> None:
        self.assertEqual(self.check("/nonexistent/ruleset.json").returncode, MALFORMED)
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "bad.json"
            path.write_text("not json", encoding="utf-8")
            self.assertEqual(self.check(path).returncode, MALFORMED)


class UsageTests(unittest.TestCase):
    def run_script(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(SCRIPT), *args], capture_output=True, text=True, check=False
        )

    def test_no_mode_is_a_usage_error(self) -> None:
        self.assertEqual(self.run_script().returncode, USAGE)
        self.assertEqual(self.run_script("--nonsense").returncode, USAGE)

    def test_export_all_requires_a_directory(self) -> None:
        self.assertEqual(self.run_script("--export-all").returncode, USAGE)
        self.assertEqual(self.run_script("--export-all", "/nonexistent/dir").returncode, USAGE)


if __name__ == "__main__":
    unittest.main()
