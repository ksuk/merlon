# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Release notes for each tagged version are generated from the corresponding
section below, so every release must have one.

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
  matching against the last successful list on fetch failure, and an
  operational alert after repeated failures.
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

### Removed

- The Rust rule-evaluation engine and its Protocol Buffers/gRPC interface,
  including `MonitoringService`, superseded by the native Go engine
  (ADR-0013, superseding ADR-0002).

### Known limitations

- No bulk data import. Initial migration of an existing customer master and
  transaction history runs through the REST API; see
  `docs/operations/initial-migration.md` for the supported procedure and its
  constraints, and ADR-0015 for the bulk loader design.
- Production release is gated on the governance controls in
  `docs/development/release-checklist.md`, which are not yet evidenced.
