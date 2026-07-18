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
