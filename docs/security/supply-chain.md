---
title: Supply Chain
---

# Supply Chain

How Merlon's dependencies, build inputs, and published artifacts are
controlled, and what evidence exists for each control. Written for a reviewer
who needs to record findings, not for a maintainer.

## Pinning

Every build input is pinned to an immutable identifier.

| Input | Pinned as | Where |
|---|---|---|
| Base images (Go, Node.js, Alpine, PostgreSQL) | Tag **and** `sha256` digest | `api/Dockerfile`, compose files, `.github/workflows/ci.yml` |
| GitHub Actions | Full commit SHA, with the version in a trailing comment | All workflows |
| Go modules | `go.sum` | `api/go.sum` |
| npm packages | `package-lock.json` | `ui/`, `website/` |
| Wrangler (docs deploy) | Exact version | `website/package.json`, `docs-deploy.yml` |

A pin is only worth what enforces it. The same version is deliberately declared
in more than one place — a Dockerfile and the workflow that tests it, for
example — and guard scripts fail the build when the copies disagree:

| Guard | Enforces |
|---|---|
| `scripts/check-container-pins.sh` | The PostgreSQL image digest is identical across every compose file and CI |
| `scripts/check-toolchain-pins.sh` | Go and Node.js versions match across the Dockerfile, all workflows, `go.mod`, and the dev container |
| `scripts/check-wrangler-pin.sh` | The Wrangler version in `package.json` matches the one the deploy workflow runs |
| `scripts/check-env-vars.sh` | Every environment variable the code reads is documented, and every documented variable is read |

Each guard fails if it finds **zero** occurrences of what it is checking, not
just on a mismatch. A control that silently stops checking anything when a step
is renamed is worse than no control, because it reports success.

These run as required checks on every pull request.

## Dependency updates

Dependabot runs monthly in three review lanes — application, documentation,
infrastructure — each limited to one open pull request, with cooldown periods
before a newly published version is proposed (14 days by default, 60 for
majors). Security updates are enabled separately and are not delayed by these
schedules.

Runtime end-of-life dates are tracked explicitly in
[Dependency Lifecycle](../operations/dependency-lifecycle.md), reviewed
quarterly, and re-checked against upstream sources on every release date.

## Vulnerability scanning

Runs on every pull request and weekly on a schedule:

| Scan | Covers |
|---|---|
| `gitleaks` | Committed secrets |
| `govulncheck` | Go dependencies, reachability-aware |
| `npm audit` (via `scripts/check-npm-audit.mjs`) | `ui/` and `website/` dependencies |
| `go-licenses` / `license-checker` | Licence allowlist for Go and npm dependencies |
| `anchore/sbom-action` | CycloneDX SBOM for the API, UI, and website |

### Accepted npm advisories

Advisories that cannot be resolved immediately are recorded in
`scripts/npm-audit-exceptions.json`. An entry is not a suppression; it must
carry a reachability rationale, the dependents it was assessed against, and an
**expiry date**.

The gate fails when an exception expires, when the advisory's scope has changed
from what was assessed, or when the advisory no longer exists — that last case
matters because it means a stale exception is silently covering nothing.

## Build and release

Publishing is triggered only by an annotated Git tag, and the workflow refuses
to proceed unless:

1. The tag is strict SemVer, optionally with an `-alpha.N`/`-beta.N`/`-rc.N`
   pre-release identifier.
2. The tag is **annotated**, not lightweight.
3. The tagged commit is an **ancestor of `main`**.
4. `CHANGELOG.md` has a section for that version.

Nothing is published from a branch head. There is no `latest` or rolling tag.

The checks that can reject a release all run *before* the image is pushed,
deliberately: once an image is public under a release tag, a later failure
cannot unpublish it.

### What each release produces

| Artifact | Purpose |
|---|---|
| Multi-architecture image (`linux/amd64`, `linux/arm64`) | The software |
| GitHub build provenance attestation, pushed to the registry | Ties the image digest to the workflow, repository, and commit that built it |
| CycloneDX SBOM of the image | Component inventory for your own scanning |
| `release-manifest.json` | Tag, commit, image, digest, SBOM hash, provenance URL |
| `SHA256SUMS` | Integrity of the attached files |

[Upgrading](../operations/upgrade.md) has the consumer-side verification
procedure. Verify before deploying; the artifacts are only worth something if
somebody checks them.

## Reproducing the image

The image builds from the repository with no private inputs:

```bash
docker build -f api/Dockerfile \
  --build-arg VERSION=vX.Y.Z \
  --build-arg REVISION="$(git rev-parse HEAD)" \
  -t merlon:verify .
```

Neither builder stage runs under emulation — the Go binary cross-compiles and
the UI bundle is architecture-neutral — so the `linux/arm64` image is produced
by the same code path as `linux/amd64`, not by a separate emulated build.

## Known gaps

Recorded here rather than omitted, because a reviewer will find them anyway:

| Gap | Status |
|---|---|
| Images are attested but not signed with cosign/sigstore | Provenance attestation covers origin; a detached signature does not currently exist |
| SBOMs are generated but not scanned in CI | They are published for you to scan; no gate consumes them yet |
| No static application security testing (CodeQL or equivalent) | Not currently configured |
| No published container-image CVE scan | Scan the published SBOM or image yourself |
| One active maintainer | Stated in `MAINTAINERS.md`; production release is gated on resolving it |

See [Accepted Risks](accepted-risks/index.md) for the ones that are deliberate
rather than pending.
