---
title: Container Images
---

# Container Images

Merlon publishes one image, to GitHub Container Registry:

```
ghcr.io/ksuk/merlon
```

It contains the API server and the operator UI. The Go binary serves the UI
from `MERLON_UI_DIR`, so there is no separate web-server container. PostgreSQL
is not bundled; you supply it.

## Tags

| Tag | Points to | Immutable | Suitable for |
|---|---|---|---|
| `vX.Y.Z` | A production release | Yes | Production, after your own assessment |
| `vX.Y.Z-rc.N` | A pre-release build for evaluation | Yes | Evaluation, testing, demos |
| `@sha256:...` | One exact build | Yes | Any deployment you want pinned |

Two things are deliberately absent.

**There is no `latest` tag.** A mutable tag means two hosts that ran the same
`docker pull` on different days can be running different software while
reporting the same version, which is not a defensible position for a system
that produces regulatory records. Release identity here is the digest; the
version tag is a convenience that points at one.

**There is no rolling `main` or `dev` tag.** Every published image corresponds
to an annotated, protected tag that is an ancestor of `main`, and to a
`CHANGELOG.md` section. Nothing is published from a branch head.

### `vX.Y.Z-rc.N` is not a lesser artifact

A pre-release is built by the same workflow, on the same multi-architecture
build, with the same provenance attestation, SBOM, and evidence manifest. The
artifacts are identical in kind.

The difference is what the tag asserts about the *project*, not about the
build: a production release additionally asserts the governance and operational
controls in the [release checklist](../development/release-checklist.md) —
an independent release approver, a recorded restore exercise, a recorded
vulnerability-response exercise. Until those are evidenced, this repository
publishes pre-releases only.

For an evaluation, a proof of concept, or a test environment, that distinction
does not matter. For production, read the checklist and decide against your own
regulatory obligations.

## Pulling

Pull by tag to see what you got, then redeploy by digest:

```bash
docker pull ghcr.io/ksuk/merlon:v0.1.0-rc.1
docker inspect --format '{{index .RepoDigests 0}}' ghcr.io/ksuk/merlon:v0.1.0-rc.1
```

Verify before running it. Every release attaches `release-manifest.json`,
`sbom-image.cdx.json`, and `SHA256SUMS`, and the image carries a GitHub build
provenance attestation. [Upgrading](upgrade.md) has the full verification
procedure.

## Architectures

`linux/amd64` and `linux/arm64`. Both come from one build: the Go binary is
cross-compiled and the UI bundle is architecture-neutral, so neither
architecture is produced under emulation and neither is a second-class build.

## What is in the image

| Property | Value |
|---|---|
| Base | `alpine` (pinned by tag and digest) |
| Runs as | uid/gid `10001`, non-root |
| Writable paths needed | None — runs unmodified with `--read-only` |
| Exposed port | `8080` |
| Healthcheck | `GET /healthz/live` (liveness), honouring `MERLON_HTTP_ADDR` |
| Outbound network | Only what you configure; see [Data Egress](../security/data-egress.md) |

The built-in healthcheck is a liveness probe, so a fresh container reports
`healthy` as soon as the process is serving. Readiness — initial setup
complete, database reachable, engine loaded — is exposed separately at
`GET /healthz/ready` for orchestration probes and for compose healthchecks that
deliberately gate on a usable instance.

All state lives in PostgreSQL. The container holds no data, so replacing it is
never a data-loss event — see [Backup and Restore](backup-restore.md) for what
does need backing up.

### Image metadata

Standard OCI annotations are set, including
`org.opencontainers.image.revision`, which ties a pulled image back to the
commit its provenance attestation was issued for:

```bash
docker inspect ghcr.io/ksuk/merlon:v0.1.0-rc.1 \
  --format '{{json .Config.Labels}}'
```

## Building it yourself

The image is reproducible from the repository, and the BUSL grant permits it:

```bash
docker build -f api/Dockerfile \
  --build-arg VERSION=local \
  --build-arg REVISION="$(git rev-parse HEAD)" \
  -t merlon:local .
```

Passing `VERSION` matters: without it the binary reports `dev` at
`GET /healthz`, and an image that cannot state its own version is not one you
want to find in an audit trail.
