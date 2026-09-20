"""Guard the evidence collected by the Docker Demo browser verifier."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]


class DemoTourEvidenceContractTests(unittest.TestCase):
    def test_verifier_checks_release_identity_and_download_contents(self):
        verifier = (ROOT / "scripts" / "verify-demo-tour.mjs").read_text(encoding="utf-8")
        wrapper = (ROOT / "scripts" / "verify-demo-tour.sh").read_text(encoding="utf-8")

        for marker in (
            "EXPECTED_VERSION",
            "EXPECTED_REVISION",
            "createHash",
            "format=json",
            "format=csv",
            "format=yaml",
            "rule YAML export failed",
            "reconciliation_delta",
        ):
            self.assertIn(marker, verifier)
        self.assertIn("-e EXPECTED_VERSION", wrapper)
        self.assertIn("-e EXPECTED_REVISION", wrapper)


if __name__ == "__main__":
    unittest.main()
