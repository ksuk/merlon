---
title: Upgrade Runbook
---

# Upgrade Runbook

## Before upgrading

1. Read the [release notes](../release-notes.md) for every version between the
   one you are running and the one you are moving to, and back up the
   PostgreSQL database and encryption-key material.
2. Test the upgrade in an environment containing a representative copy of
   production configuration and data.
3. Record the current application version and native engine configuration
   digests (`GET /api/v1/system/config-digests`).

## Verify the release artifacts

Each tagged release publishes a container image plus the evidence needed to
confirm you are deploying what the project built. Verify it before rolling
out — deploying an unverified image discards the whole provenance chain.

Download `release-manifest.json`, `sbom-image.cdx.json`, and `SHA256SUMS`
from the GitHub release, then:

```bash
# 1. The attached files match their published checksums.
sha256sum -c SHA256SUMS

# 2. Read the immutable image digest out of the manifest. Deploy by digest,
#    never by a mutable tag.
IMAGE=$(jq -r .image release-manifest.json)
DIGEST=$(jq -r .image_digest release-manifest.json)

# 3. GitHub attests that this exact digest was built by this repository's
#    release workflow.
gh attestation verify "oci://${IMAGE}@${DIGEST}" --repo ksuk/merlon

# 4. Pull the verified digest.
docker pull "${IMAGE}@${DIGEST}"
```

Deploy `${IMAGE}@${DIGEST}` and record that digest in your deployment record.
The release manifest also names the release commit, so the deployed artifact
can be traced back to source.

## Apply migrations

Set `MERLON_MIGRATION_DATABASE_URL` to a dedicated schema-owner connection and
run:

```bash
make migrate
```

The migration runner records every filename and checksum in
`schema_migrations`, takes an advisory lock, and applies each file in its own
transaction. A second run is a no-op; a checksum mismatch stops the rollout.
For a database that predates the ledger, set
`MERLON_MIGRATION_BASELINE=<last-applied-filename>` explicitly after verifying
the backup. The runner never infers a baseline from table contents.

## Rollback

SQL migrations are forward-only unless a release-specific rollback is supplied. If validation fails, stop the rollout, restore the pre-upgrade backup, and investigate before retrying. Do not delete migration history or edit an already-applied migration.
