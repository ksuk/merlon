#!/usr/bin/env python3
"""Validate linked release inputs and create the release evidence manifest."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
from pathlib import Path
import re
import sys
from typing import Any
from urllib.parse import urlparse


SEMVER = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
COMMIT = re.compile(r"^[0-9a-f]{40}$")
DIGEST = re.compile(r"^sha256:([0-9a-f]{64})$")
STATEMENT_TYPE = "https://in-toto.io/Statement/v1"
PROVENANCE_TYPE = "https://slsa.dev/provenance/v1"


class EvidenceError(ValueError):
    """The supplied evidence cannot support the manifest claim."""


def load_json(path: Path, description: str) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise EvidenceError(f"invalid {description} {path}: {exc}") from exc


def decode_statement(bundle_path: Path) -> dict[str, Any]:
    bundle = load_json(bundle_path, "provenance bundle")
    envelope = bundle.get("dsseEnvelope") if isinstance(bundle, dict) else None
    if envelope is None or not isinstance(envelope.get("payload"), str):
        raise EvidenceError("provenance bundle has no DSSE payload")
    signatures = envelope.get("signatures")
    if not isinstance(signatures, list) or not any(
        isinstance(signature, dict) and bool(signature.get("sig"))
        for signature in signatures
    ):
        raise EvidenceError("provenance bundle has no DSSE signature")
    payload = envelope["payload"]
    try:
        padded = payload + "=" * (-len(payload) % 4)
        statement = json.loads(base64.b64decode(padded, validate=True))
    except (ValueError, UnicodeError, json.JSONDecodeError) as exc:
        raise EvidenceError(f"invalid provenance DSSE payload: {exc}") from exc
    if not isinstance(statement, dict):
        raise EvidenceError("provenance DSSE payload is not a statement")
    return statement


def validate_inputs(args: argparse.Namespace) -> str:
    if not SEMVER.fullmatch(args.tag):
        raise EvidenceError("tag must be strict vMAJOR.MINOR.PATCH")
    if not COMMIT.fullmatch(args.commit):
        raise EvidenceError("commit must be a lowercase 40-character SHA")
    digest_match = DIGEST.fullmatch(args.image_digest)
    if digest_match is None:
        raise EvidenceError(
            "image digest must be sha256 followed by 64 lowercase hex characters"
        )
    if not args.image or any(character.isspace() for character in args.image):
        raise EvidenceError("image name must be non-empty and contain no whitespace")

    provenance_url = urlparse(args.provenance_url)
    if provenance_url.scheme != "https" or not provenance_url.netloc:
        raise EvidenceError("provenance URL must be an absolute HTTPS URL")

    sbom = load_json(args.sbom, "SBOM")
    if not isinstance(sbom, dict) or sbom.get("bomFormat") != "CycloneDX":
        raise EvidenceError("SBOM must be CycloneDX JSON")
    if not isinstance(sbom.get("specVersion"), str) or not sbom["specVersion"]:
        raise EvidenceError("CycloneDX SBOM must declare specVersion")

    statement = decode_statement(args.provenance_bundle)
    if statement.get("_type") != STATEMENT_TYPE:
        raise EvidenceError("provenance statement has an unsupported in-toto type")
    if statement.get("predicateType") != PROVENANCE_TYPE:
        raise EvidenceError("provenance statement is not SLSA provenance v1")

    expected_hex = digest_match.group(1)
    subjects = statement.get("subject")
    if not isinstance(subjects, list) or not any(
        isinstance(subject, dict)
        and subject.get("name") == args.image
        and isinstance(subject.get("digest"), dict)
        and subject["digest"].get("sha256") == expected_hex
        for subject in subjects
    ):
        raise EvidenceError("provenance subject does not match the image name and digest")

    predicate = statement.get("predicate")
    build_definition = (
        predicate.get("buildDefinition") if isinstance(predicate, dict) else None
    )
    dependencies = (
        build_definition.get("resolvedDependencies")
        if isinstance(build_definition, dict)
        else None
    )
    if not isinstance(dependencies, list) or not any(
        isinstance(dependency, dict)
        and isinstance(dependency.get("digest"), dict)
        and dependency["digest"].get("gitCommit") == args.commit
        for dependency in dependencies
    ):
        raise EvidenceError("provenance commit does not match the manifest commit")
    return hashlib.sha256(args.sbom.read_bytes()).hexdigest()


def sha256_line(path: Path) -> str:
    return f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n"


def create_evidence(args: argparse.Namespace) -> None:
    sbom_sha256 = validate_inputs(args)
    args.output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = args.output_dir / "release-manifest.json"
    checksums_path = args.output_dir / "SHA256SUMS"
    manifest = {
        "schema_version": 2,
        "tag": args.tag,
        "commit": args.commit,
        "image": args.image,
        "image_digest": args.image_digest,
        "sbom": {"file": args.sbom.name, "sha256": sbom_sha256},
        "provenance": {
            "provider": "GitHub artifact attestation",
            "url": args.provenance_url,
        },
        "governance": {
            "mode": "single-maintainer",
            "independent_approval": False,
            "separation_of_duties": False,
            "adr": "ADR-0016",
        },
    }
    manifest_path.write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8", newline="\n"
    )
    checksums_path.write_text(
        sha256_line(manifest_path) + sha256_line(args.sbom),
        encoding="utf-8",
        newline="\n",
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--image-digest", required=True)
    parser.add_argument("--sbom", required=True, type=Path)
    parser.add_argument("--provenance-bundle", required=True, type=Path)
    parser.add_argument("--provenance-url", required=True)
    parser.add_argument("--output-dir", required=True, type=Path)
    return parser.parse_args()


def main() -> int:
    try:
        create_evidence(parse_args())
    except EvidenceError as exc:
        print(f"release evidence error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
