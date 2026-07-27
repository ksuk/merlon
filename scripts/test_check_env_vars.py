"""Integration tests for the environment-variable drift guard."""

from __future__ import annotations

import pathlib
import shutil
import subprocess
import tempfile
import textwrap
import unittest


SCRIPT = pathlib.Path(__file__).with_name("check-env-vars.sh")


class EnvVarGuardTests(unittest.TestCase):
    def run_guard(self, documented_vars: tuple[str, ...]) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "scripts").mkdir()
            (root / "api" / "cmd").mkdir(parents=True)
            (root / "docs").mkdir()
            shutil.copyfile(SCRIPT, root / "scripts" / SCRIPT.name)

            (root / "api" / "cmd" / "main.go").write_text(
                textwrap.dedent(
                    """\
                    package main

                    import "os"

                    func readConfig(getenv func(string) string) {
                        _ = os.Getenv("MERLON_DATABASE_URL")
                        _ = getenv("MERLON_MIGRATIONS_DIR")
                    }
                    """
                ),
                encoding="utf-8",
            )
            (root / "docs" / "configuration.md").write_text(
                "\n".join(f"`{name}`" for name in documented_vars),
                encoding="utf-8",
            )
            (root / ".env.example").write_text(
                "\n".join(
                    (
                        "MERLON_DATABASE_URL=postgres://example",
                        "# MERLON_BACKUP_DATABASE_URL=postgres://backup",
                        "MERLON_POSTGRES_PASSWORD=example",
                    )
                ),
                encoding="utf-8",
            )

            return subprocess.run(
                ["bash", str(root / "scripts" / SCRIPT.name)],
                cwd=root,
                check=False,
                capture_output=True,
                text=True,
            )

    def test_lowercase_getenv_variable_must_be_documented(self) -> None:
        result = self.run_guard(("MERLON_DATABASE_URL",))

        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("MERLON_MIGRATIONS_DIR", result.stderr)

    def test_documented_lowercase_getenv_variable_passes(self) -> None:
        result = self.run_guard(
            ("MERLON_DATABASE_URL", "MERLON_MIGRATIONS_DIR"),
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("2 read, 2 documented", result.stdout)


if __name__ == "__main__":
    unittest.main()
