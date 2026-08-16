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
| `vX.Y.Z` | A release | Yes | Any deployment, after your own assessment |
| `@sha256:...` | One exact build | Yes | Any deployment you want pinned |

Two things are deliberately absent.

**There is no `latest` tag.** A mutable tag means two hosts that ran the same
`docker pull` on different days can be running different software while
reporting the same version, which is not a defensible position for a system
that produces regulatory records. Release identity here is the digest; the
version tag is a convenience that points at one.

**There is no rolling `main` or `dev` tag, and no pre-release channel.** Every
published image corresponds to an annotated, protected tag that is an ancestor
of `main`, and to a `CHANGELOG.md` section. Nothing is published from a branch
head, and a tag carrying a pre-release identifier is rejected rather than
published.

### What a release tag asserts, and what it does not

There used to be a second channel here whose job was to signal, through its
name, that the project's governance controls were incomplete. A tag suffix is a
weak way to say that: readers skip it, and its meaning is only in the
documentation anyway. The same facts now travel with the artifact, in a form
you can query.

This project has one maintainer. A `vX.Y.Z` image therefore asserts that every
automated gate passed on the release commit and that a self-review record
exists for the changes it contains. It does **not** assert independent approval
or separation of duties — one person cannot review their own work
independently — and it says so on itself:

```bash
docker inspect ghcr.io/ksuk/merlon:v0.1.0 \
  --format '{{json .Config.Labels}}' | jq 'with_entries(select(.key | startswith("io.github.ksuk.merlon.governance")))'
```

```json
{
  "io.github.ksuk.merlon.governance.mode": "single-maintainer",
  "io.github.ksuk.merlon.governance.independent-approval": "false",
  "io.github.ksuk.merlon.governance.separation-of-duties": "false",
  "io.github.ksuk.merlon.governance.adr": "ADR-0016"
}
```

The same four facts are in `release-manifest.json` on every GitHub release, and
in a header above the release notes. The build itself is not weaker for it: the
same multi-architecture build, provenance attestation, SBOM, and evidence
manifest as any other release.

Whether that is good enough to deploy is your assessment to make against your
own regulatory obligations — and for an AML/CFT system, one you have to be able
to defend to a regulator regardless of what any vendor claims. The reasoning
behind the disclosure is in
[Single-Maintainer Operating Mode](../development/repository-governance.md) and
ADR-0016.

## Pulling

Pull by tag to see what you got, then redeploy by digest:

```bash
docker pull ghcr.io/ksuk/merlon:v0.1.0
docker inspect --format '{{index .RepoDigests 0}}' ghcr.io/ksuk/merlon:v0.1.0
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
| Healthcheck | `GET /healthz/live` (liveness), honouring the listener selected by `MERLON_MODE` |
| Outbound network | Only what you configure; see [Data Egress](../security/data-egress.md) |

The built-in healthcheck is a liveness probe, so a fresh container reports
`healthy` as soon as the process is serving. Readiness — initial setup
complete, database reachable, engine loaded — is exposed separately at
`GET /healthz/ready` for orchestration probes and for compose healthchecks that
deliberately gate on a usable instance.

In `worker` mode the probe uses `MERLON_WORKER_HTTP_ADDR` (default `:8081`);
in `api` and `all` modes it uses `MERLON_HTTP_ADDR` (default `:8080`).
Wildcard, IPv4/IPv6, and host-qualified listen addresses are normalized into a
valid loopback or host URL before invoking the probe.

All state lives in PostgreSQL. The container holds no data, so replacing it is
never a data-loss event — see [Backup and Restore](backup-restore.md) for what
does need backing up.

### Image metadata

Standard OCI annotations are set, including
`org.opencontainers.image.revision`, which ties a pulled image back to the
commit its provenance attestation was issued for:

```bash
docker inspect ghcr.io/ksuk/merlon:v0.1.0 \
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
