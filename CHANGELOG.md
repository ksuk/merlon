# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Release notes for each tagged version are generated from the corresponding
section below, so every release must have one. There is no fallback: a tag
without its own section is rejected, and pre-release tags are not published at
all.

## [Unreleased]

No version has been tagged yet. Everything below describes the state of `main`
ahead of the first release.

### Breaking

Three deliberate contract changes ship in the Wave 3 operator workflows. Each
withdraws an ability or corrects a field whose name did not match its meaning,
so none has an additive alternative.

1. **`POST /api/v1/customers/{id}/score` now requires the `cdd:score`
   permission** (ADR-0019). The CDD score decides EDD requirements, monitoring
   thresholds and rescreening frequency, so producing one is a control action.
   Admin and Analyst hold the permission; **Viewer receives 403**.
   *Migration:* move Viewer-role integrations that rescore customers to
   Analyst. Deployments running without authentication are unaffected -- with
   no roles configured there is nothing to check.

2. **`factors[].score` changed meaning** (ADR-0019). It previously held the
   same number as `contribution`, so summing it double-counted the factor
   weighting. `score` is now the factor's own normalised value (0-10) and
   `contribution` is its weighted share of the total.
   *Migration:* a client that summed `factors[].score` to reconstruct the
   total must read `factors[].contribution` instead. The record-level `score`
   is unchanged. `GET /customers/{id}/score-explanation` now reports
   `reconciled` and `reconciliation_delta` so the arithmetic can be verified
   rather than assumed.

3. **`POST /api/v1/batch/runs/{id}/rerun` returns an unconfirmed manifest**
   (ADR-0018). It previously cloned the target manifest as already-confirmed
   and executed it immediately, which let any population be re-run with no
   second look and no second person -- bypassing the preview-and-confirm
   control that the target mechanism exists to provide.
   *Migration:* the response body is now
   `{target_manifest, operation, parameters, rerun_of, next}`. Confirm the
   returned manifest with `POST /api/v1/batch/targets/{id}/confirm` using its
   token, then start the run with `POST /api/v1/batch/runs`.

Two further changes are 2xx-compatible but worth noting:

- `POST /api/v1/batch/runs` returns **202 Accepted** rather than 201. The run
  is started and its row committed; execution continues independently of the
  request, so a client disconnect no longer strands it at `status=running`.
  Poll `GET /api/v1/batch/runs/{id}` for the outcome.
- `investigation.edd.completion_status` gains `overdue` and `completed`
  (ADR-0021). Both are refinements of states previously reported as `open` or
  `escalated`.

### Added

- CDD risk scoring with configurable weights, country risk tables, and risk
  tiers, driving TM thresholds, case priority, and screening frequency
  (ADR-0004, Score-Driven Architecture).
- Transaction monitoring engine with structuring, rapid movement, dormant
  account reactivation, high-frequency small-amount, and high-risk country
  transfer scenarios.
- `tm_scenario_v2` scenario schema with per-customer-type and per-risk-tier
  thresholds, evaluation modes, and absolute thresholds, with dual support for
  the earlier `tm_scenario_v1` format (ADR-0006).
- Sanctions and PEP screening with scheduled list imports that continue
  matching against the last successful list on fetch failure, and flags repeated
  failures for operators (structured log, dashboard flag, staleness metric).
- Backtesting against candidate rule sets, including affected-customer
  reporting and cancellation.
- Alert and case management: bulk alert close and case assignment, case notes,
  related-case linking, alert suppression, and STR report generation and
  export.
- Dual-path authentication with role-based permissions and dual control on
  rule activation and whitelist approval (ADR-0007, ADR-0014).
- Encryption at rest for direct-PII customer attributes, applied in the
  repository layer so no write path can bypass it, with online key rotation
  (`merlon-keyrotate`) that re-encrypts in batches without downtime.
- Append-only audit log with partitioning and a `merlon-audit verify`
  integrity check (ADR-0010, ADR-0011).
- Webhook delivery with a dead-letter queue and reprocessing.
- Cursor-based pagination across list endpoints, with the offset/limit
  contract retained during its deprecation period (ADR-0005).
- REST API surface with a generated OpenAPI 3.0 document.
- React UI (Vite, React 19, Tailwind CSS v4, React Router v8).
- PostgreSQL schema with a checksum-verified, forward-only migration runner.
- Docker Compose topologies (minimal, standard, development, demo) and a
  synthetic demo dataset generator.
- Release workflow publishing a multi-architecture image with build
  provenance attestation, a CycloneDX SBOM, and a release evidence manifest.
- Single release channel `vX.Y.Z`, with the project's governance posture
  published alongside every release rather than encoded in a tag suffix:
  `release-manifest.json` carries a `governance` block, the image carries
  matching `io.github.ksuk.merlon.governance.*` labels, and the release notes
  carry a disclosure header. All three record that the release does not assert
  independent approval or separation of duties (ADR-0016).
- `Governance Required` check: every pull request must carry a self-review
  record posted by its author and bound to its head commit, verified by
  `scripts/check-self-review.mjs`. A record from anyone else is ignored and
  deleting the record turns the check red again, so the gate cannot be
  satisfied by a passer-by on a public repository. It replaces the approving
  review a single-maintainer repository cannot produce, and is never described
  as one.
- Ruleset drift detection: the `main` and release-tag rulesets are committed to
  `.github/rulesets/` and compared weekly against the live configuration.
  Rulesets deleted from the live configuration are reported as drift alongside
  modified and unknown ones.
- Release gate verifying that `CI Required` and `Security Required` concluded
  successfully on the release commit before any image is published, so the
  disclosure header the release carries is checked rather than asserted.
- Container image hardening: runs as non-root uid 10001, needs no writable
  path (`--read-only` works unmodified), declares a `/healthz/live` liveness
  healthcheck — readiness is exposed separately at `/healthz/ready` — and
  carries OCI annotations including the build revision.
- Troubleshooting guide with a symptom index keyed on the actual error
  strings, and an FAQ covering the design decisions evaluators ask about.
- Security and assurance documentation for vendor review: a complete
  enumeration of outbound connections, supply-chain controls and their known
  gaps, and accepted risks with their compensating controls.
- `make backup` / `make restore`, which capture the database and the
  encryption key ring as separate artifacts and refuse to produce a
  database-only backup silently.
- Guards for environment-variable drift (`make verify-env-vars`) and OpenAPI
  coverage (`make verify-openapi-coverage`), both required in CI.
- Bilingual (English/Japanese) documentation site with generated API and rule
  schema reference pages.
- Versioned, operator-editable policy documents for KYC required fields, the
  EDD stage schedule, CDD rule-set selection, Travel Rule applicability, and
  screening source readiness, with read-only `GET /api/v1/policies` and
  `GET /api/v1/policies/{policy}` (ADR-0016). Their digests are pinned onto
  the runs and batches they governed.
- Server-side Travel Rule assessment recorded on every transaction, including
  those submitted without a counterparty block (ADR-0017). A client assertion
  that disagrees with the policy is preserved and the conflict recorded rather
  than silently corrected, and evidence that is required but absent routes the
  transaction to the PENDING_REVIEW queue instead of rejecting it.
- Explicit EDD completion (`POST /api/v1/customers/{id}/edd/{complete,reopen}`)
  with an append-only event history, `overdue`/`completed` states, and
  `overdue_days` (ADR-0021). A tier downgrade now closes an EDD window while
  retaining its evidence rather than erasing the stage timestamps.
- Maker-checker for CDD tier overrides: an override becomes a proposal that a
  second person approves via
  `POST /api/v1/customers/{id}/score-overrides/{id}/approve` (ADR-0019), plus
  `GET /api/v1/customers/{id}/cdd-rule-sets` reporting which rule sets apply
  and which the policy recommends.
- Batch run cancellation (`POST /api/v1/batch/runs/{id}/cancel`), retaining
  work already completed as a terminal state distinct from failed and partial.
- Monitoring gap queue export (`GET /api/v1/pending-evaluations/export`, CSV
  and JSON, gated by `audit:read`).
- Screening runs and results now record when they were made against a stale,
  failed or never-imported watchlist, so a clear result cannot be mistaken for
  evidence of absence, and `GET /api/v1/screening/sources` reports
  `screening_ready`.

### Changed

- Backend consolidated into a single Go service. Rule evaluation now runs as
  a native in-process engine rather than a separate service (ADR-0013).
- Canonical repository URLs and Go import paths now use
  `github.com/ksuk/merlon`.
- JSON Schema identifiers now use the repository-based
  `https://github.com/ksuk/merlon/schemas/` namespace.
- Documentation deployment now targets Cloudflare Workers instead of GitHub
  Pages.
- Rate limiting is now proxy-aware, deriving the client address from trusted
  forwarding headers.
- The documentation sidebar is now organized by audience rather than by
  directory, and the rule schema reference no longer presents configuration
  schema versions as REST API versions.

### Fixed

- The retention purger could not delete any customer created after the Wave 3
  schema landed: three foreign keys were added without matching guards, so
  `DELETE FROM customers` raised a foreign-key violation that aborted the whole
  purge transaction. One unpurgeable customer stopped retention for every
  customer. Four further foreign keys had never been guarded. The guarded list
  is now checked against the live PostgreSQL catalogue by an integration test
  (ADR-0020).
- Append-only evidence tables could never be purged, because the trigger
  rejected every UPDATE and DELETE. Retention and tamper-evidence were
  mutually unsatisfiable obligations; a purge-aware guard now permits exactly
  the retention lifecycle and nothing else (ADR-0020).
- Batch target resolution read a single page of customers and filtered that,
  so a filter matching customers beyond the first page produced a manifest
  covering only some of them -- with no error and a `target_count` an operator
  would read as complete. Resolution now walks the whole book.
- A manual batch run executed on the request context, so a client disconnect
  cancelled both the execution and its finalisation, leaving the run at
  `status=running` with its target manifest consumed until the process
  restarted.
- The monitoring gap recovery sweep read one fixed page of a newest-first
  queue, so a backlog deeper than one page left its oldest records -- the ones
  that had waited longest -- permanently unprocessed.
- A FAILED monitoring gap could be revived by the ordinary retry action
  without limit, which made the automatic retry budget meaningless. Reviving is
  now a separate action requiring approval authority and counted separately.
- Repeat screening hits on a list entry the same customer had already been
  cleared against re-entered the queue on every rescreen with no sign they had
  been judged before.
- Screening source state was classified three different ways by three
  implementations, which disagreed; a recorded successful import whose body is
  missing now reports as unreadable everywhere rather than as fresh.
- `GET /api/v1/backtests/{id}/affected-customers` rebuilt its whole answer from
  the job's stored result on every request, and omitted entirely the customers
  a candidate rule set would stop alerting on.
- `GET /api/v1/system/info` reported a hardcoded version of `1.0.0` and a
  hardcoded endpoint count that had drifted to roughly half the real API
  surface. Both are now measured from the running build.
- The OpenAPI document declared a server URL of `/api/v1` while its path keys
  already carried that prefix, so every effective URL was
  `/api/v1/api/v1/...`. A client generated from the published document would
  have called paths that do not exist. It also reported a hardcoded API
  version rather than the build's.
- `docker-compose.dev.yml` targeted a build stage that does not exist, so
  `make dev-up` could not start. It now builds the Go toolchain stage and runs
  from source.
- The first-run path was undocumented: `/setup` creates the initial
  administrator but was named nowhere, leaving a new deployment at a login
  screen with no account. It is now covered in the quick start, in the
  authorization guide, and linked from the login screen.
- `README.md` stated that enterprise features are controlled by a license key.
  No such mechanism exists.

### Removed

- The Rust rule-evaluation engine and its Protocol Buffers/gRPC interface,
  including `MonitoringService`, superseded by the native Go engine
  (ADR-0013, superseding ADR-0002).
- `ui/Dockerfile` and its nginx dependency. The operator UI is served by the
  Go binary from `MERLON_UI_DIR`, so no compose file or workflow ever built
  this image.
- `docker-compose.minimal.yml`, which was a duplicate of `docker-compose.yml`.
  The standard topology is now the default, so `docker compose up` needs no
  `-f` flag. `make minimal-up` / `make minimal-down` become `make up` /
  `make down`.

### Known limitations

- No bulk data import. Initial migration of an existing customer master and
  transaction history runs through the REST API; see
  `docs/operations/initial-migration.md` for the supported procedure and its
  constraints, and ADR-0015 for the bulk loader design.
- Production release is gated on the governance controls in
  `docs/development/release-checklist.md`, which are not yet evidenced.
