---
sidebar_position: 2
title: Architecture
---

# Architecture

Merlon is a self-hosted AML/CFT application designed primarily for Japanese
non-bank financial institutions. This document is for developers and operators
who need to understand its component boundaries and operational controls.

```
External systems ── REST/webhooks ──> Go API ── gRPC ──> Rust Engine
                                         │
                                         └──────────────> PostgreSQL
React operator UI ─────────────── REST ────────────────> Go API
```

## Components

- **React UI (`ui/`)** provides the operator dashboard.
- **Go API (`api/`)** accepts customer and transaction data, exposes REST
  endpoints, manages cases and reports, and orchestrates background work.
- **Rust Engine (`engine/`)** evaluates CDD scoring, transaction-monitoring
  rules, screening lists, and backtests through gRPC.
- **PostgreSQL** persists operational records, score history, and audit logs.
  Redis and object storage are optional deployment integrations.

The Protocol Buffers definitions in `proto/` are the compatibility boundary
between the Go API and Rust Engine. Changes must remain additive to preserve
client compatibility.

## Design principles

1. **Auditability first** — retain decision evidence and traceable operations.
2. **Configuration as the product** — express rules as versioned YAML/JSON
   configuration rather than application code.
3. **Score-driven architecture** — CDD score informs monitoring and review
   priority.
4. **Adapter isolation** — normalize external-system differences at the edge.
5. **Secure by default** — production Engine connections require TLS; secrets
   and metrics endpoints must be protected by deployment controls.
6. **Contract stability** — retain external contract compatibility for at
   least 12 months.
7. **Fail-alert** — prefer reviewable alerts over silent missed detections.

## Operational boundaries

One deployment serves one institution. Merlon does not claim regulatory
compliance for every jurisdiction: deploying organizations own their legal
assessment, rule governance, secrets management, backup/restore process, and
control of Engine configuration files. See the
[deployment runbook](operations/deployment.md) and
ADR-0012.
