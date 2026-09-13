#!/usr/bin/env python3
"""Contract tests for release evidence generation and rehearsal wiring."""

from __future__ import annotations

import base64
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "release_evidence.py"
DRY_RUN_WORKFLOW = ROOT / ".github" / "workflows" / "release-evidence-dry-run.yml"
RELEASE_WORKFLOW = ROOT / ".github" / "workflows" / "release.yml"
RELEASE_CHECKLIST = ROOT / "docs" / "development" / "release-checklist.md"


class ReleaseEvidenceTest(unittest.TestCase):
    image = "ghcr.io/ksuk/merlon"
    digest = "sha256:" + "a" * 64
    commit = "b" * 40

    def write_inputs(
        self,
        directory: Path,
        *,
        subject_digest: str | None = None,
        bom_format: str = "CycloneDX",
        signed: bool = True,
        provenance_commit: str | None = None,
    ) -> tuple[Path, Path]:
        sbom = directory / "sbom-image.cdx.json"
        sbom.write_text(
            json.dumps(
                {
                    "bomFormat": bom_format,
                    "specVersion": "1.6",
                    "version": 1,
                    "components": [],
                }
            ),
            encoding="utf-8",
        )

        digest = subject_digest or self.digest
        statement = {
            "_type": "https://in-toto.io/Statement/v1",
            "subject": [
                {
                    "name": self.image,
                    "digest": {"sha256": digest.removeprefix("sha256:")},
                }
            ],
            "predicateType": "https://slsa.dev/provenance/v1",
            "predicate": {
                "buildDefinition": {
                    "resolvedDependencies": [
                        {
                            "uri": "git+https://github.com/ksuk/merlon@refs/heads/main",
                            "digest": {"gitCommit": provenance_commit or self.commit},
                        }
                    ]
                },
                "runDetails": {},
            },
        }
        payload = base64.b64encode(
            json.dumps(statement, separators=(",", ":")).encode("utf-8")
        ).decode("ascii")
        bundle = directory / "provenance.bundle.json"
        bundle.write_text(
            json.dumps(
                {
                    "mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
                    "dsseEnvelope": {
                        "payload": payload,
                        "signatures": ([{"sig": "test-signature"}] if signed else []),
                    },
                }
            ),
            encoding="utf-8",
        )
        return sbom, bundle

    def run_generator(
        self,
        directory: Path,
        *,
        subject_digest: str | None = None,
        bom_format: str = "CycloneDX",
        signed: bool = True,
        provenance_commit: str | None = None,
    ) -> subprocess.CompletedProcess[str]:
        sbom, bundle = self.write_inputs(
            directory,
            subject_digest=subject_digest,
            bom_format=bom_format,
            signed=signed,
            provenance_commit=provenance_commit,
        )
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "--tag",
                "v0.0.1",
                "--commit",
                self.commit,
                "--image",
                self.image,
                "--image-digest",
                self.digest,
                "--sbom",
                str(sbom),
                "--provenance-bundle",
                str(bundle),
                "--provenance-url",
                "https://github.com/ksuk/merlon/attestations/1234",
                "--output-dir",
                str(directory),
            ],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )

    def test_generates_linked_manifest_and_checksums(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            result = self.run_generator(directory)

            self.assertEqual(result.returncode, 0, result.stderr)
            manifest_path = directory / "release-manifest.json"
            checksums_path = directory / "SHA256SUMS"
            manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

            self.assertEqual(manifest["schema_version"], 2)
            self.assertEqual(manifest["tag"], "v0.0.1")
            self.assertEqual(manifest["commit"], self.commit)
            self.assertEqual(manifest["image"], self.image)
            self.assertEqual(manifest["image_digest"], self.digest)
            self.assertEqual(manifest["sbom"]["file"], "sbom-image.cdx.json")
            self.assertEqual(
                manifest["sbom"]["sha256"],
                hashlib.sha256(
                    (directory / "sbom-image.cdx.json").read_bytes()
                ).hexdigest(),
            )
            self.assertEqual(
                manifest["provenance"],
                {
                    "provider": "GitHub artifact attestation",
                    "url": "https://github.com/ksuk/merlon/attestations/1234",
                },
            )
            self.assertEqual(
                manifest["governance"],
                {
                    "mode": "single-maintainer",
                    "independent_approval": False,
                    "separation_of_duties": False,
                    "adr": "ADR-0016",
                },
            )

            checksum_lines = checksums_path.read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(checksum_lines), 2)
            self.assertEqual(
                checksum_lines[0],
                f"{hashlib.sha256(manifest_path.read_bytes()).hexdigest()}  "
                "release-manifest.json",
            )
            self.assertEqual(
                checksum_lines[1],
                f"{hashlib.sha256((directory / 'sbom-image.cdx.json').read_bytes()).hexdigest()}  "
                "sbom-image.cdx.json",
            )

    def test_rejects_provenance_for_another_digest_without_outputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            result = self.run_generator(
                directory, subject_digest="sha256:" + "c" * 64
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("provenance subject", result.stderr)
            self.assertFalse((directory / "release-manifest.json").exists())

    def test_rejects_unsigned_provenance_without_outputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            result = self.run_generator(directory, signed=False)

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("signature", result.stderr)
            self.assertFalse((directory / "release-manifest.json").exists())

    def test_rejects_provenance_for_another_commit_without_outputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            result = self.run_generator(directory, provenance_commit="c" * 40)

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("provenance commit", result.stderr)
            self.assertFalse((directory / "release-manifest.json").exists())
            self.assertFalse((directory / "SHA256SUMS").exists())

    def test_rejects_non_cyclonedx_sbom_without_outputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            result = self.run_generator(directory, bom_format="SPDX")

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("CycloneDX", result.stderr)
            self.assertFalse((directory / "release-manifest.json").exists())


class ReleaseWorkflowContractTest(unittest.TestCase):
    def test_dry_run_is_manual_non_publishing_and_retains_evidence(self):
        workflow = DRY_RUN_WORKFLOW.read_text(encoding="utf-8")
        trigger = workflow.split("permissions:", maxsplit=1)[0]

        self.assertIn("workflow_dispatch:", workflow)
        self.assertNotIn("\n  push:", trigger)
        self.assertIn("push: false", workflow)
        self.assertIn("push-to-registry: false", workflow)
        self.assertNotIn("packages: write", workflow)
        self.assertNotIn("docker/login-action", workflow)
        self.assertIn('git merge-base --is-ancestor "$candidate" origin/main', workflow)
        self.assertIn("scripts/release_evidence.py", workflow)
        self.assertIn("provenance.bundle.json", workflow)
        self.assertIn("release-manifest.json", workflow)
        self.assertIn("sbom-image.cdx.json", workflow)
        self.assertIn("SHA256SUMS", workflow)
        self.assertIn("release-evidence-environment.txt", workflow)
        self.assertNotIn("gh release create", workflow)

    def test_publishing_workflow_uses_the_same_evidence_generator(self):
        workflow = RELEASE_WORKFLOW.read_text(encoding="utf-8")

        self.assertIn("scripts/release_evidence.py", workflow)
        self.assertNotIn("jq -n", workflow)

    def test_release_checklist_documents_the_rehearsal_boundary(self):
        checklist = RELEASE_CHECKLIST.read_text(encoding="utf-8")
        normalized = " ".join(checklist.split())

        self.assertIn("Release Evidence Dry Run", normalized)
        self.assertIn("does not create a tag", normalized)
        self.assertIn("does not push an image", normalized)
        self.assertIn("does not publish a GitHub release", normalized)


if __name__ == "__main__":
    unittest.main()
