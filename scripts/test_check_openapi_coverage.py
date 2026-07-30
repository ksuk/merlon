"""Tests for the OpenAPI coverage ratchet."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import pathlib
import tempfile
import unittest
from unittest import mock


SCRIPT = pathlib.Path(__file__).with_name("check-openapi-coverage.py")
SPEC = importlib.util.spec_from_file_location("check_openapi_coverage", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
coverage = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(coverage)


def route_source(count: int, *, replace_last: bool = False) -> str:
    routes = [
        f's.route("GET /api/v1/example-{index}", handler)'
        for index in range(count)
    ]
    if replace_last:
        routes[-1] = 's.route("POST /api/v1/replacement", handler)'
    return "\n".join(routes)


def openapi_spec(
    documented: int,
    *,
    stale: bool = False,
    swap_documented: bool = False,
) -> dict:
    paths = {
        f"/api/v1/example-{index}": {"get": {"responses": {"200": {}}}}
        for index in range(documented)
    }
    if swap_documented:
        del paths[f"/api/v1/example-{documented - 1}"]
        paths[f"/api/v1/example-{documented}"] = {
            "get": {"responses": {"200": {}}}
        }
    if stale:
        paths["/api/v1/stale"] = {"get": {"responses": {"200": {}}}}
    return {"openapi": "3.0.3", "servers": [{"url": "/"}], "paths": paths}


class CoverageRatchetTests(unittest.TestCase):
    def run_guard(
        self,
        *,
        registered: int = coverage.BASELINE_REGISTERED,
        documented: int = coverage.BASELINE_DOCUMENTED,
        stale: bool = False,
        replace_registered: bool = False,
        swap_documented: bool = False,
    ) -> tuple[int, str, str]:
        baseline_registered = coverage.registered_routes(
            route_source(coverage.BASELINE_REGISTERED)
        )
        baseline_documented = coverage.documented_routes(
            openapi_spec(coverage.BASELINE_DOCUMENTED)
        )
        baseline_undocumented = baseline_registered - baseline_documented

        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            routes_file = root / "server.go"
            spec_file = root / "openapi.json"
            routes_file.write_text(
                route_source(registered, replace_last=replace_registered),
                encoding="utf-8",
            )
            spec_file.write_text(
                json.dumps(
                    openapi_spec(
                        documented,
                        stale=stale,
                        swap_documented=swap_documented,
                    )
                ),
                encoding="utf-8",
            )

            stdout = io.StringIO()
            stderr = io.StringIO()
            with (
                mock.patch.object(coverage, "ROUTES_FILE", routes_file),
                mock.patch.object(coverage, "SPEC_FILE", spec_file),
                mock.patch.object(
                    coverage,
                    "BASELINE_REGISTERED_SHA256",
                    coverage.route_set_digest(baseline_registered),
                ),
                mock.patch.object(
                    coverage,
                    "BASELINE_UNDOCUMENTED_SHA256",
                    coverage.route_set_digest(baseline_undocumented),
                ),
                contextlib.redirect_stdout(stdout),
                contextlib.redirect_stderr(stderr),
            ):
                status = coverage.main()

        return status, stdout.getvalue(), stderr.getvalue()

    def test_current_registered_and_documented_baselines_pass(self) -> None:
        status, stdout, stderr = self.run_guard()

        self.assertEqual(status, 0, stderr)
        self.assertIn("OpenAPI coverage: 44/78", stdout)

    def test_undocumented_registered_route_addition_fails(self) -> None:
        status, _, stderr = self.run_guard(
            registered=coverage.BASELINE_REGISTERED + 1,
        )

        self.assertEqual(status, 1)
        self.assertIn("registered route count rose to 79", stderr)

    def test_same_count_registered_route_replacement_fails(self) -> None:
        status, _, stderr = self.run_guard(replace_registered=True)

        self.assertEqual(status, 1)
        self.assertIn(
            "registered route set changed while the count remained 78",
            stderr,
        )
        self.assertIn("POST /api/v1/replacement", stderr)

    def test_same_count_undocumented_route_replacement_fails(self) -> None:
        status, _, stderr = self.run_guard(swap_documented=True)

        self.assertEqual(status, 1)
        self.assertIn(
            "undocumented route set changed while the count remained 34",
            stderr,
        )
        self.assertIn("GET /api/v1/example-43", stderr)

    def test_registered_route_removal_requires_baseline_update(self) -> None:
        status, _, stderr = self.run_guard(
            registered=coverage.BASELINE_REGISTERED - 1,
        )

        self.assertEqual(status, 1)
        self.assertIn("registered route count fell to 77", stderr)

    def test_documented_route_addition_requires_baseline_update(self) -> None:
        status, _, stderr = self.run_guard(
            documented=coverage.BASELINE_DOCUMENTED + 1,
        )

        self.assertEqual(status, 1)
        self.assertIn("coverage rose to 45", stderr)

    def test_documented_route_removal_points_to_spec_source(self) -> None:
        status, _, stderr = self.run_guard(
            documented=coverage.BASELINE_DOCUMENTED - 1,
        )

        self.assertEqual(status, 1)
        self.assertIn("api/internal/server/openapi.go", stderr)
        self.assertNotIn("api/cmd/openapi-export", stderr)

    def test_stale_documented_route_fails(self) -> None:
        status, _, stderr = self.run_guard(stale=True)

        self.assertEqual(status, 1)
        self.assertIn("documented in openapi.json but not registered", stderr)
        self.assertIn("GET /api/v1/stale", stderr)


if __name__ == "__main__":
    unittest.main()
