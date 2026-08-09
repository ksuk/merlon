"""Regression tests for Make entry-point wiring."""

from __future__ import annotations

import pathlib
import subprocess
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class MakeTargetTests(unittest.TestCase):
    def run_make(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["make", *args],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
        )

    def test_openapi_verification_generates_the_ignored_artifact_first(self) -> None:
        result = self.run_make(
            "--dry-run",
            "verify-openapi-coverage",
            "VERSION=review-test",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        # The ldflags carry commit and build time alongside the version, and
        # their values depend on the checkout, so this asserts the parts that
        # matter rather than the whole string: the version reaches the
        # generator, the generator writes the artifact, and it runs before the
        # verifier reads it.
        generator = "./cmd/openapi-export -o ../docs/api/openapi.json"
        verifier = "python3 scripts/check-openapi-coverage.py"
        self.assertIn(
            "github.com/ksuk/merlon/api/internal/buildinfo.Version=review-test",
            result.stdout,
        )
        self.assertIn(generator, result.stdout)
        self.assertIn(verifier, result.stdout)
        self.assertLess(result.stdout.index(generator), result.stdout.index(verifier))

    def test_restore_force_maps_only_true_to_force_flag(self) -> None:
        forced = self.run_make(
            "--dry-run",
            "restore",
            "BACKUP_FILE=backup.dump",
            "RESTORE_FORCE=true",
        )
        self.assertEqual(forced.returncode, 0, forced.stderr)
        self.assertIn('scripts/restore.sh "backup.dump" --force', forced.stdout)

        normal = self.run_make(
            "--dry-run",
            "restore",
            "BACKUP_FILE=backup.dump",
        )
        self.assertEqual(normal.returncode, 0, normal.stderr)
        restore_line = next(
            line for line in normal.stdout.splitlines() if "scripts/restore.sh" in line
        )
        self.assertNotIn("--force", restore_line)

    def test_restore_rejects_ambiguous_force_values_before_running_script(self) -> None:
        result = self.run_make(
            "restore",
            "BACKUP_FILE=backup.dump",
            "RESTORE_FORCE=yes",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RESTORE_FORCE must be true or unset", result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
