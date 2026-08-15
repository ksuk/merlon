---
title: Backtest Outcome Analysis and Known-Matter Coverage
sidebar_position: 11
---

# Backtest Outcome Analysis and Known-Matter Coverage

Backtest jobs expose an additive `outcome_analysis` projection. The shared
`outcome-matcher-v2` contract evaluates baseline, candidate, and delta alerts
against immutable historical alert decisions, cases, and submitted STRs.

## Rates and evidence boundary

Each variant reports `tp`, `fp`, `unlabeled`, `unevaluable`, `investigated`,
`rate`, and `denominator`. Only TP and FP are in the denominator. An event-time
score tier is required; a missing historical score remains `unevaluable` rather
than falling back to the customer's current tier. Every detail row carries the
matcher version, snapshot, assumptions, and source provenance.

`GET /api/v1/backtests/{id}/outcomes` returns the durable detail stream. It
supports `variant`, `scenario_id`, `label`, and cursor pagination.

## Known-matter coverage

`POST /api/v1/coverage-analyses` queues a durable
`comparison/known_matter_coverage` job and requires the half-open replay period
(`from`, `to`). The worker replays the pinned rule set over transactions visible
at `snapshot_at`, then builds a deterministic union
of internal evidence, preferring (in order) qualifying escalated/STR-filed
cases, submitted STRs, and case-unlinked closed true-positive alerts. Linked
rows are deduplicated with the case as the primary matter.

`GET /api/v1/coverage-analyses/{id}` reports overall and scenario summaries;
`GET /api/v1/coverage-analyses/{id}/matters` returns each matter with its source,
coverage state, matcher assumptions, and snapshot provenance. Scenario matching
uses the candidate scenario union within a customer. The report covers only
matter known to the institution's internal records; it does not estimate
unobserved events. `not_covered` and `unevaluable` remain separate counts.

The UI repeats this boundary and always shows the denominator, matcher version,
replay period, rule set, snapshot, and assumptions alongside the rate.
