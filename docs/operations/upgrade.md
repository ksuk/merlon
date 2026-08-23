---
title: Upgrade Runbook
---

# Upgrade Runbook

Merlon holds no state in the container. Upgrading means replacing an image;
your data stays in PostgreSQL where it was. What makes an upgrade consequential
is not the image swap — it is the schema migration, which is forward-only.

## Choose an approach first

| Situation | Approach |
|---|---|
| Local evaluation | Pull the newest version tag, upgrade freely |
| Shared or test environment | Pin a version tag, upgrade deliberately, keep a backup |
| Production | Pin the **digest**, rehearse on a copy of production data, back up immediately before, never upgrade two versions blind |

There is no automatic update and nothing will notify you that a release exists
— see [Data Egress](../security/data-egress.md) for why. Watch
[Releases](https://github.com/ksuk/merlon/releases).

## 1. Before upgrading

1. Read the [release notes](../release-notes.md) for **every** version between
   the one you are running and the one you are moving to, not just the target.
2. **Back up the database and the encryption key ring**
   ([Backup and Restore](backup-restore.md)). This is your only rollback.
3. Record the current version (`GET /healthz`) and the native engine
   configuration digests (`GET /api/v1/system/config-digests`).
4. Rehearse in an environment holding a representative copy of production
   configuration and data.

## 2. Verify the release artifacts

Every tagged release publishes an image plus the evidence needed to confirm you
are deploying what the project built. Verify before rolling out — deploying an
unverified image discards the entire provenance chain.

Download `release-manifest.json`, `sbom-image.cdx.json`, and `SHA256SUMS` from
the GitHub release, then:

```bash
# 1. The attached files match their published checksums.
sha256sum -c SHA256SUMS

# 2. Read the immutable image digest out of the manifest. Deploy by digest,
#    never by a mutable tag.
IMAGE=$(jq -r .image release-manifest.json)
DIGEST=$(jq -r .image_digest release-manifest.json)

# 3. GitHub attests that this exact digest was built by this repository's
#    release workflow, from the commit named in the manifest.
gh attestation verify "oci://${IMAGE}@${DIGEST}" --repo ksuk/merlon

# 4. Pull the verified digest.
docker pull "${IMAGE}@${DIGEST}"
```

Deploy `${IMAGE}@${DIGEST}` and record that digest in your deployment record.
See [Container Images](container-images.md) for what each tag means.

## 3. Apply migrations

Migrations do not run at startup. They are a separate, deliberate step using a
role the serving role does not have:

```bash
export MERLON_MIGRATION_DATABASE_URL='postgres://merlon_migrate:...@host:5432/merlon'
make migrate
make audit-harden
```

The runner records every filename and SHA-256 in `schema_migrations`, takes an
advisory lock, and applies each file in its own transaction. A second run is a
no-op. A checksum mismatch stops the rollout — see
[Troubleshooting: Database](../troubleshooting/database.md).

`make audit-harden` is the required second step. It grants the serving role
only the reviewed access for every application table, including tables just
created by the target release, while keeping audit evidence append-only and
the migration ledger owner-only. Run it with the same
`MERLON_MIGRATION_DATABASE_URL`; set `MERLON_APP_ROLE` when the serving role is
not the default `merlon_app`.

:::warning Rolling upgrades are not supported across a schema change

Running the old and new versions against the same database at the same time
will fail: the old code does not understand the new schema. Take the old
version out of service, migrate, reapply the serving-role grants, then bring
the new one up.

:::

## 4. Verify the upgrade

```bash
# The version you intended.
curl -s http://localhost:8080/healthz

# Every subsystem ready — not just the process alive.
curl -s http://localhost:8080/healthz/ready

# Engine configuration digests match what you recorded in step 1,
# or changed for a reason you can name.
curl -s http://localhost:8080/api/v1/system/config-digests

# The audit chain is intact across the migration.
cd api && go run ./cmd/merlon-audit verify
```

Then exercise a representative workflow: score a customer, review an alert,
open a case. A green health check means the process started, not that scoring
still behaves the way your rules expect.

## 5. Rolling back

:::danger Migrations are one-way

SQL migrations are forward-only unless a release-specific rollback is supplied.
Reverting the container image does **not** revert the schema. If a migration
has been applied, the only way back is to restore the pre-upgrade backup — and
that loses everything written since it was taken.

:::

If validation fails:

1. Stop the rollout. Do not apply further migrations.
2. If no migration was applied, redeploy the previous digest. That is the whole
   rollback.
3. If a migration was applied, create a fresh database, restore and validate
   the pre-upgrade backup there ([Backup and Restore](backup-restore.md)), then
   cut `MERLON_DATABASE_URL` over and redeploy the previous digest. Never
   restore in place over the current schema.
4. Investigate before retrying.

Never delete rows from `schema_migrations` or edit an already-applied migration
to "unstick" a rollout. That does not reconcile the schema, it only removes the
evidence that it diverged.
