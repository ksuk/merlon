# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Release notes for each tagged version are generated from the corresponding
section below, so every release must have one. Pre-release tags
(`vX.Y.Z-rc.N` and friends) do not need a section of their own: they publish
the section for the release they are a candidate for, or `[Unreleased]`.

## [Unreleased]

No version has been tagged yet. Everything below describes the state of `main`
ahead of the first release.

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
- Pre-release channel: `vX.Y.Z-rc.N` tags publish the same attested,
  multi-architecture image as a production release, marked as a GitHub
  pre-release. Governance and operational-evidence controls apply to `vX.Y.Z`
  only, so the software can be evaluated from a published image while those
  controls are being established.
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
