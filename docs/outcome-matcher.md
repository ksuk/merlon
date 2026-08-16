---
title: Shared Outcome Matcher
sidebar_position: 10
---

# Shared Outcome Matcher

The `api/internal/outcome` package is the single deterministic matching
contract used by backtest outcome analysis and known-matter coverage.

## Contract

- Alerts are the primary unit. A customer-period view is a consumer-level
  aggregation, not a second matching algorithm.
- A candidate and reference must share a customer. Backtest mode also requires
  the same scenario; coverage mode permits a scenario-union match.
- Non-empty transaction sets use Jaccard similarity. If the transaction sets
  are absent or weaker than the interval signal, the matcher uses overlap
  coefficient for the two detection windows. A score of `0.50` is inclusive.
- Assignment is one-to-one and sorted by score descending, time delta
  ascending, then candidate and reference IDs.
- Labels are `TP`, `FP`, `unlabeled`, and `unevaluable`. Unlabeled and
  unevaluable rows are retained for audit but excluded from the rate
  denominator. Missing event-time score history is unevaluable; the current
  customer tier is never used as a fallback.

Every result carries `matcher_version`, the assumptions, the as-of snapshot,
source provenance, and the denominator used by rate calculations.
