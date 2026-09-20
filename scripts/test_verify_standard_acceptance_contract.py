"""Contract checks for the Standard topology browser acceptance verifier."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]


class StandardAcceptanceContractTest(unittest.TestCase):
    def test_make_target_runs_standard_acceptance_wrapper(self) -> None:
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("verify-standard-acceptance:", makefile)
        self.assertIn("bash scripts/verify-standard-acceptance.sh", makefile)

    def test_wrapper_owns_fresh_standard_stack_and_restart(self) -> None:
        wrapper = (ROOT / "scripts" / "verify-standard-acceptance.sh").read_text(encoding="utf-8")
        for marker in (
            "docker-compose.yml",
            "MERLON_OPERATOR_CONTENT_PATH",
            'up --build --detach',
            'restart api',
            'down --volumes --remove-orphans',
            'run_verifier before-restart',
            'run_verifier after-restart',
        ):
            self.assertIn(marker, wrapper)

    def test_browser_verifier_covers_security_and_authenticated_contract(self) -> None:
        verifier = (ROOT / "scripts" / "verify-standard-acceptance.mjs").read_text(encoding="utf-8")
        for marker in (
            "/setup",
            "/login",
            "/users",
            "analyst",
            "viewer",
            "selectOption",
            "/api/v1/openapi.json",
            "X-CSRF-Token",
            "after-restart",
        ):
            self.assertIn(marker, verifier)


if __name__ == "__main__":
    unittest.main()
