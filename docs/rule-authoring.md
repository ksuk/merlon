---
title: Rule Authoring Guide
sidebar_position: 6
---

# Rule Authoring Guide

This guide is for compliance officers and rule authors who define Merlon's
CDD (customer due diligence) risk weights, country risk tables, and
transaction-monitoring (TM) scenarios. Under the **Configuration as the
Product** design principle, these rules are expressed as versioned JSON/YAML
configuration rather than application code, so they can be authored, reviewed,
and approved through your institution's own rule-governance process instead
of a software release.

## Where rules live

Rule content lives under `content/` in the repository:

- `content/schema/` — the JSON Schemas that validate each rule type.
- `content/_sample/` — Apache-2.0-licensed sample rules that demonstrate the
  format. They are not production-ready controls; the sample directory's
  `README.md` and every sample file mark this explicitly (`is_sample: true`
  where applicable).

The native Go engine loads CDD weights, country risk tables, and TM scenarios
directly from operator-supplied files — for example, `MERLON_CDD_WEIGHTS_PATH`
and `MERLON_TM_SCENARIOS_PATH` (see
[Configuration Reference](./configuration.md)). Because these files sit
outside the database-backed rule audit trail, your organization is
responsible for their source control, change approval, access control, and
backup, as described in ADR-0012 (Engine Configuration File Trust Boundary).
The engine emits a deterministic digest of the loaded configuration at startup
so a deployed rule set can be verified after the fact — this supports audit,
but does not replace file-level access control.

## CDD weight definitions

A CDD weight definition assigns weighted risk factors ("axes") to a customer
and derives a risk tier from the combined score. The schema is
`content/schema/cdd_weight_v1.json`
([reference](./api/schema/cdd_weight_v1.md)). A minimal, real example is
`content/_sample/cdd_basic_weights.yaml`:

```yaml
schema_version: cdd_weight_v1
id: cdd_basic_weights
name: Basic CDD Risk Weights
version: 1
is_sample: true

axes:
  customer_attributes:
    weight: 0.25
    factors:
      - name: customer_type
        scores:
          individual: 1
          corporate_domestic: 2
          corporate_foreign: 4
      - name: account_age_months
        ranges:
          - max: 3
            score: 4
          - max: 12
            score: 2
          - min: 12
            score: 1

  geography:
    weight: 0.30
    factors:
      - name: country_risk
        scores:
          low: 1
          medium: 3
          high: 5

tier_thresholds:
  low:
    max_score: 2.0
  medium:
    min_score: 2.0
    max_score: 3.5
  high:
    min_score: 3.5
```

Required fields per the schema: `schema_version` (must be the literal
`cdd_weight_v1`), `id` (lowercase, `^[a-z][a-z0-9_]*$`), `name`, `version`
(a positive integer), `axes`, and `tier_thresholds` (must define `low`,
`medium`, and `high`). Each entry under `axes` requires a `weight` (0–1) and a
`factors` array; factors may score a value directly (`scores`, keyed by an
observed value) or score a numeric range (`ranges`, each with `min`/`max`
bounds and a `score`).

A factor can also delegate scoring to the country risk table instead of an
inline `scores` map, by setting its `source` to `country_risk_table`
(see the native Go country-risk implementation) — use this
when a factor's risk-by-country logic should be maintained in one place
rather than duplicated across every CDD weight definition.

Set `is_sample: false` (or omit it) once you replace a sample with a
production rule, per the guidance in `content/README.md`.

## Country risk tables

A country risk table assigns a numeric risk score (1–5) per ISO country code,
with a default for countries not explicitly listed. The schema is
`content/schema/country_risk_v1.json`
([reference](./api/schema/country_risk_v1.md)). Example, from
`content/_sample/country_risk_sample.yaml`:

```yaml
schema_version: "1.0"
content_type: country_risk_table
name: Sample Country Risk Table
effective_date: 2026-07-01
default_score: 3
countries:
  JP: { score: 1 }
  US: { score: 1 }
  KP: { score: 5, reason: "FATF blacklist / foreign exchange sanctions" }
  IR: { score: 5, reason: "FATF blacklist" }
  MM: { score: 4, reason: "FATF grey list" }
```

Required fields: `schema_version`, `content_type` (the literal constant
`country_risk_table`), `effective_date` (a date), `default_score` (1–5), and
`countries`. Each country key must match `^[A-Z]{2}$` (an ISO 3166-1 alpha-2
code) and its entry requires a `score`; `reason` is optional but recommended
for traceability of why a jurisdiction was scored the way it was, in keeping
with the Auditability First principle.

## TM scenarios

A TM (transaction monitoring) scenario defines a detection rule — for
example, an aggregation-based structuring check — and the severity of an
alert it produces. **Author new scenarios against the v2 schema**,
`content/schema/tm_scenario_v2.json`
([reference](./api/schema/tm_scenario_v2.md)). Example, adapted from
`content/_sample/tm_scenarios/structuring_basic.yaml`:

```yaml
schema_version: "2.0"
scenario_id: tm_structuring_basic
name: "Structuring Detection (Basic)"
description: "Detects transaction splitting intended to evade reporting thresholds"
type: aggregation
conditions:
  transaction_type:
    - deposit
    - transfer_in
  aggregation:
    field: amount
    function: sum
    period: 24h
    group_by: customer_id
  threshold:
    by_customer_type:
      individual:
        by_risk_tier:
          LOW: 2000000
          MEDIUM: 1000000
          HIGH: 500000
      corporate_domestic:
        by_risk_tier:
          LOW: 2000000
          MEDIUM: 1000000
          HIGH: 500000
  additional:
    min_transaction_count: 3
evaluation_mode: both
severity: HIGH
tags:
  - structuring
```

Required fields: `schema_version`, `scenario_id`, `name`, `type` (currently
only `aggregation` is defined), `conditions`, and `severity`. Under
`conditions`, `threshold.by_customer_type` lets a scenario set a different
threshold per customer type, and within each customer type,
`by_risk_tier.{LOW,MEDIUM,HIGH}` sets thresholds by the customer's CDD risk
tier — this is the concrete mechanism behind the Score-Driven Architecture
principle: a customer's CDD score determines which TM threshold applies to
them. `evaluation_mode` controls whether the scenario runs in the batch job,
inline, or both; if omitted, v2 scenarios default to `batch`.

### v1 content and dual support

Existing `tm_scenario_v1` content is not being force-migrated. Per
ADR-0006 (tm_scenario_v2 Schema and v1 Dual Support), the Engine
dual-supports v1 content for at least 12 months from the v2 rollout,
internally converting v1's flat `risk_tier_adjustments` into the equivalent
"same threshold for every customer type" v2 shape at load time, without
changing the resulting evaluation semantics. `content/schema/tm_scenario_v1.json`
([reference](./api/schema/tm_scenario_v1.md)) remains available for that
compatibility period. New scenarios should still be authored directly against
v2 rather than v1.

## Validating rule files

Validate every rule file against its schema before deploying it — this is a
mechanical check that should run as part of your review or CI process, ahead
of any organizational approval step. The schema reference pages under
[Rule Schemas](./api/schema/index.md) document every field and constraint for
`cdd_weight_v1`, `country_risk_v1`, `tm_scenario_v1`, and `tm_scenario_v2` and
are generated directly from the JSON Schema files in `content/schema/`, so
they stay in sync with what the Engine actually enforces.

## Score-driven rollout

Because CDD score is the central axis of the system (see
[Architecture](./architecture.md) and ADR-0004, Score-Driven Architecture),
a change to a CDD
weight definition can shift which risk tier a customer falls into, which in
turn changes which TM thresholds apply to them, their case priority, and
their screening frequency. Treat CDD weight changes with the same or greater
caution as TM scenario changes, since their effect is indirect but system-wide
rather than confined to a single detection rule.

The `docs/configuration.md` operational-thresholds guidance applies equally
here: the sample rules in `content/_sample/` are examples, not production
defaults. Choose thresholds from your institution's own documented risk
assessment, test changes before deploying them, and route changes through
your rule-governance process. Under the **Fail-Alert** principle, resolve
uncertainty about a new or changed rule toward additional review rather than
suppressed alerts — for example, prefer a wider net (more matches reviewed
and dismissed) over a narrower one (fewer alerts but a higher chance of a
missed detection) when a threshold choice is ambiguous.
