# PH9 parity corpus

This directory is the T1/T3 gate for the native engine. The committed corpus
was captured from the pre-consolidation implementation before the Go engine was
made authoritative. Each record contains one operation, its normalized input,
and the expected output. `parity_test.go` replays every record through Go.

Coverage checklist:

- scoring: every supported customer type and risk tier, country-risk default
  and boundary values, resolved and fallback factors, and multiple rule sets;
- monitoring: all five scenarios in realtime and batch mode, exact/±1
  thresholds, empty input, same-second events, and a 10,000-event window;
- screening: exact, near, kana/Japanese, empty, punctuation, whitespace, and
  Unicode normalization cases;
- backtest: multiple customers and scenarios, including a missing-tier
  customer (the `MEDIUM` fallback);
- config validation: valid YAML plus every validation error path.

Comparison normalization is deliberately narrow: timestamp fields,
time-derived IDs, and execution durations are excluded; alerts are sorted by
`(customer_id, scenario_id, first_transaction_id)`; floating-point values are
compared by IEEE-754 bits, never by formatted text. Descriptions remain exact
string comparisons, including `*.5` rounding boundaries.

The corpus and replay test are a required CI gate. Keep this frozen fixture when
changing the native engine so that the pre-consolidation behavior remains
auditable.

## Refrozen 2026-08-07 (Wave 3, ADR-0019)

The two `scoring` records were re-frozen when `domain.Factor.Score` stopped
duplicating `Contribution`. `Score` is now the factor's own normalised value
and `Contribution` is its weighted share of the total; previously both held
the contribution, so a consumer that summed `score` double-counted the
weighting. The record-level `score` is unchanged, which is the point: the
totals this corpus exists to protect did not move, only the per-factor
breakdown became able to explain them.

`rule_set_version` in these records is now `0` rather than a fingerprint of
the digest. The column carries a real rule-set version everywhere else, and
writing a hash into it made an unversioned score look like version
1,750,295,863. The pin is `rule_set_sha256`, which is unchanged.
