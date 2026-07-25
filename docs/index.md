---
title: Overview
sidebar_position: 0
---

# Overview

Merlon is self-hosted AML/CFT software for non-bank financial institutions in
Japan. It scores customer risk (CDD), monitors transactions (TM), screens
against sanctions and PEP lists, and manages the resulting alerts and cases —
all running inside your own infrastructure, against your own PostgreSQL
database.

These pages are organized by what you are trying to do. Pick the track that
matches your role; each one is ordered so you can read it top to bottom.

## Understand what Merlon does

Start here regardless of your role.

- [Getting Started](getting-started.md) — run the whole stack with Docker
  Compose in about five minutes
- [Merlon Demo Tour](demo-tour.md) — walk a pre-loaded dataset through
  scoring, alerts, and case resolution
- [Architecture](architecture.md) — how the Go API, native engine, and React
  UI fit together, and why the CDD score is the central axis

## Compliance and controls

For compliance officers and second-line reviewers: what Merlon does about
your regulatory obligations, and what evidence it produces.

- [Regulatory Scope](compliance/regulatory-scope.md) — which obligations
  Merlon addresses, and which it deliberately does not
- [FSA Guideline Mapping](compliance/fsa-guideline-mapping.md) — Merlon's
  controls mapped to the FSA AML/CFT guidelines
- [Data Retention Policy](compliance/data-retention.md) — retention periods
  and the statutes behind them
- [Case Management Workflow](case-management.md) — the alert-to-case
  lifecycle and the STR filing path
- [Authorization and Segregation of Duties](auth.md) — roles, permissions,
  and the dual-control points

## Configure and tune

For the people who decide how strictly Merlon detects. Every rule is a
configuration file, not code.

- [Configuration Reference](configuration.md) — environment variables and
  `config.yaml`
- [Rule Authoring Guide](rule-authoring.md) — writing CDD weights, country
  risk tables, and TM scenarios, and rolling changes out safely

## Deploy and operate

For whoever runs Merlon in production.

- [Deployment Runbook](operations/deployment.md) — production topology and
  rollout
- [API and Worker Mode](operations/worker-mode.md) — splitting the API from
  background job processing
- [Initial Data Migration](operations/initial-migration.md) — loading your
  existing customers and transaction history from files
- [Upgrade Runbook](operations/upgrade.md) — moving to a new release and
  verifying what you deployed
- [Backup and Restore Runbook](operations/backup-restore.md)
- [Partitioning and Capacity Operations Guide](operations/partitioning-guide.md)
- [Dependency Lifecycle](operations/dependency-lifecycle.md) — supported
  runtime versions and EOL tracking
- [Release Notes](release-notes.md) — what changed in each version

## Integrate and extend

For developers connecting Merlon to a core banking system or another
upstream.

- [Adapter Guide](adapter-guide.md) — mapping your source system onto
  Merlon's data model
- [REST API Reference](api/openapi.md) — every endpoint, generated from the
  route definitions
- [Rule Schemas](api/schema/index.md) — the JSON Schemas every rule
  configuration file is validated against

## Contribute

For people changing Merlon itself.

- [Development Environment Setup](development/setup.md)
- [Testing Guide](development/testing.md)
- [Documentation Guide](development/documentation.md) — how these docs are
  written, translated, and checked
- [Change Traceability](development/change-traceability.md)
- [Repository Governance](development/repository-governance.md)
- [Release Checklist](development/release-checklist.md)
