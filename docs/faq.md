---
title: Frequently Asked Questions
---

# Frequently Asked Questions

Questions about what Merlon is, and about design decisions that surprise people
evaluating it.

## About the project

### Is Merlon open source?

No, and the distinction matters for procurement.

Merlon is **source-available** under the [Business Source License 1.1](https://github.com/ksuk/merlon/blob/main/LICENSE).
The full source is public and you may read, modify, and run it — including in
production — but it is not OSI-approved open source, because the Additional Use
Grant carves out one thing: you may not offer Merlon to third parties as a
hosted, managed, or embedded compliance service.

On **2030-09-30**, the Change Date, this version becomes Apache-2.0. That is a
term of the licence, not a promise.

GitHub labels the repository's licence as "Other" because BUSL is not in its
OSI set. That is expected. The reasoning is in
[ADR-0003](https://github.com/ksuk/merlon/blob/main/docs/decisions/0003-bsl-license-choice.md).

### Can I run this in production?

The licence permits it. Whether the *project* is ready is a separate question,
and the honest answer is documented rather than marketed: see
[Container Images](operations/container-images.md) and the
[Release Checklist](development/release-checklist.md).

The repository has one active maintainer. Rather than encode that in a
pre-release tag suffix nobody reads, every release states it on the artifact:
`release-manifest.json` and the image labels record that the release does not
assert independent approval or separation of duties. Production suitability is
your assessment to make against your own regulatory obligations — and for an
AML/CFT system, that assessment is one you have to be able to defend to a
regulator regardless of what any vendor claims.

### Why is there no `latest` tag?

Because two hosts that ran the same `docker pull` on different days would then
be running different software while reporting the same version.

For a system that produces regulatory records, release identity is the image
digest. Version tags are immutable and point at one. See
[Container Images](operations/container-images.md).

### Is Merlon multi-tenant?

No. Each deployment serves one institution.

This is a deliberate constraint, not a missing feature. Multi-tenancy would put
one institution's customer data, screening results, and STR drafts in the same
database as another's, with application-level logic as the only separation.
Single-tenant deployment makes the isolation a property of the infrastructure
rather than of the code being bug-free.

## Data and privacy

### Does Merlon collect telemetry?

No. There is no usage analytics, no crash reporting, no licence check, and no
phone-home of any kind. Nothing was removed to answer this question — it was
never built.

The complete list of outbound connections Merlon can make, and what triggers
each, is in [Data Egress](security/data-egress.md). It is short, and everything
on it is something you configured.

### Does it check for updates?

No. Merlon will not tell you when a new version exists.

This has a real cost — you can run a version with a published vulnerability
without being prompted — and it is still deliberate. Merlon is frequently
deployed on closed networks, where an unexplained outbound request to a public
host is itself a finding, and where "does this software phone home" has to be
answered for every deployment rather than once.

Watch [Releases](https://github.com/ksuk/merlon/releases) instead, and see
[Upgrading](operations/upgrade.md). `GET /healthz` reports what you are running.

### Where does customer data live?

In your PostgreSQL database. The container holds no state and runs read-only.

Direct-PII customer attributes are encrypted at rest in the repository layer, so
no write path can bypass encryption. The keys live in
`MERLON_ENCRYPTION_KEY_RING`, outside the database — which means a database
backup without the matching keys is permanently unreadable. See
[Backup and Restore](operations/backup-restore.md).

## Design decisions

### Why does the CDD score drive everything?

Transaction-monitoring thresholds, case priority, and screening frequency are
all derived from the customer's CDD risk score rather than configured
independently.

The alternative — independent thresholds per subsystem — lets a customer be
high-risk for screening and low-risk for monitoring at the same time, with no
single place that explains why. Deriving them from one score means every
downstream decision has a traceable reason, which is what an examiner asks for.

See [ADR-0004](https://github.com/ksuk/merlon/blob/main/docs/decisions/0004-score-driven-architecture.md).

### Why doesn't it migrate the database automatically on startup?

Because a schema change is a change-managed event, not a side effect of a
container restart.

Migrations run as a separate operator step (`make migrate`) using a separate
database role that the serving role does not have. If the application migrated
itself at startup, the serving role would need DDL rights — including on
`audit_logs`, which the audit controls exist to keep it away from.

It also means a rollout that fails does so at a step you ran deliberately,
rather than on whichever replica happened to start first.

### Why is there no plugin system?

Because "extensible" and "runs arbitrary code inside the compliance engine" are
different things, and only one of them survives an audit.

Merlon extends through configuration:

- **Rules** are JSON/YAML in `content/`, validated against published schemas
  and versioned, with dual control on activation.
- **Integrations** are YAML-configured REST adapters — see the
  [Adapter Guide](adapter-guide.md). An adapter is configured, not coded, so
  integration logic stays outside the binary.
- **Events** leave over webhooks, with retries and a dead-letter queue.
- **Everything else** is the REST API, with API keys for non-interactive
  clients.

A plugin that executes third-party code in-process would sit inside the
transaction boundary that produces regulatory records. That is a much worse
trade for this product than for a general-purpose tool.

### Why forward-only migrations with no down scripts?

A down migration that has to reverse a data transformation is either lossy or a
lie, and you find out which during an incident.

Rollback is restore-from-backup. That is slower and it is honest, and it is why
[Backup and Restore](operations/backup-restore.md) requires a rehearsed restore
rather than a documented one.

### Why doesn't the container run as root?

It runs as uid 10001 and needs no writable path — `--read-only` works
unmodified. There is no reason for an application that writes nothing to the
filesystem to be able to.

## Documentation and contributing

### Why aren't the ADRs on this site?

The [ADRs](https://github.com/ksuk/merlon/tree/main/docs/decisions) are in the
public repository and linked from here, but they are excluded from the built
site.

They are decision records for people changing the system, written in Japanese
and assuming context that end-user documentation should not require. Publishing
them as documentation would mean maintaining them as documentation.

### Is the documentation available in Japanese?

Yes. The site is bilingual, and translation parity is enforced in CI: an English
page without a real Japanese translation fails the build unless it is explicitly
listed as an exception. The operator UI ships English and Japanese with the same
enforcement on its message catalogues.

The contributor-facing files (README, CONTRIBUTING, SECURITY) are English only.

### Can I contribute?

Yes. See
[CONTRIBUTING.md](https://github.com/ksuk/merlon/blob/main/CONTRIBUTING.md).

Two things are non-negotiable and will fail CI: every commit needs a DCO
sign-off (`git commit -s`), and every pull request must state the requirement or
issue it addresses along with a design reference. The traceability requirement
exists because this codebase produces regulatory records, and "why was this
changed" needs an answer years later.

Do not put customer data, private specifications, or internal risk assessments
in an issue or pull request.

### Something is broken. Where do I start?

[Troubleshooting](troubleshooting/index.md) opens with a symptom index — find
the message you are actually seeing rather than reading the page.
