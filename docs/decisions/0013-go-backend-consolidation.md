# ADR-0013: Backend Consolidation to Go (Supersedes ADR-0002)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-13 |
| Related ADRs | ADR-0002 (superseded), ADR-0004, ADR-0008, ADR-0012 |

## Context

ADR-0002 split the backend into a Go API and a Rust rule-evaluation engine
connected by gRPC, on the rationale that rule evaluation and backtesting are
compute-heavy and GC pauses could destabilize latency. Three developments
change that assessment.

**1. The target scale has been decided.** Merlon now explicitly targets the
largest operator tier (tier C below), while remaining deployable by the
smallest. AML/CFT transaction monitoring evaluates boundary-crossing events
(fiat and crypto-asset deposits, withdrawals, transfers) — not on-venue trade
executions, which belong to market surveillance and are out of scope. Even so,
tier C is demanding: a single arbitrage-bot customer at a crypto-asset
exchange can generate thousands of monitored transfers per day.

| Tier | Typical operator | Monitored events | Design implication |
|---|---|---|---|
| A | Small moneylenders, small funds-transfer providers | up to ~100k/day | Current design is sufficient |
| B | Mid-size funds-transfer / crypto-asset exchange | ~1M–10M/day | Needs streaming reads and SQL aggregation pushdown |
| C | Top-tier payment providers and crypto-asset exchanges; bot customers with thousands of transfers/day each | tens of millions/day | Needs partitioned parallel batch; per-customer windows of 10k+ events |

**2. An architecture review (2026-07-13) found that the binding constraints
are transport and algorithms, not implementation language.** Four defects
dominate any Go-versus-Rust performance delta:

- Backtest capacity is capped three times over: the HTTP layer hardcodes
  `maxBacktestCustomers = 100` (`api/internal/server/backtest.go`), the
  engine rejects requests above 1,000 customers / 100,000 transactions
  (`grpc/backtest_service.rs`), and behind both, `RunBacktest` ships the
  entire dataset in one unary gRPC message with no message-size overrides
  configured on either side (default ~4 MB decode cap). All three ceilings
  are artifacts of the ship-everything-per-request design.
- The nightly TM batch issues one unary RPC per customer
  (`api/internal/batch/scheduler.go`), so serialization and round-trip
  overhead scale with customer count.
- The per-customer evaluation window is capped at 1,000 rows (the
  `maxTMBatchCustomers` constant is reused as the transaction `limit`). For a
  bot customer at 5,000 transfers/day the lookback collapses to under five
  hours, making multi-day scenarios such as structuring undetectable — a
  correctness defect against the Fail-Alert principle, unreachable by any
  engine-side speedup.
- The `high_frequency_small_amount` scenario scans forward from every
  transaction, i.e. O(n²) in window size. At 10k-event windows this dwarfs
  the 2–5× language delta; the fix (two-pointer sliding window, O(n)) is
  language-independent.

The gRPC boundary also denies the engine database access, which forecloses
the two techniques tier B/C actually need: streaming evaluation and pushing
window aggregation down to PostgreSQL.

**3. The cost model changed.** LLM-assisted implementation sharply lowers the
one-time porting cost (~6,300 lines of Rust in total; ~1,900 lines of core
evaluation logic once gRPC wiring and dedicated test files are excluded),
while the recurring cost of two toolchains (triple type definitions across
proto/Go/Rust, dual CI, dual dependency hygiene) is unchanged. Additionally,
pre-publication is the only window in which the proto contract can be retired
without the 12-month backward-compatibility obligation of the Contract
Stability principle.

## Decision

1. **Consolidate the backend to a single Go codebase.** The Rust engine's
   scoring, monitoring, screening, backtest, and config-validation logic is
   ported to Go packages placed behind the existing
   `api/internal/engine/interface.go` interfaces. HTTP handlers and batch
   code do not change their call sites.
2. **Remove the gRPC/proto boundary.** The `proto/` contract, the Rust
   crates, and the Rust toolchain are deleted — but only after a golden-file
   parity gate proves the Go engine reproduces the Rust engine's outputs
   (see Consequences).
3. **Single binary, optional worker split.** One deployable binary runs in
   `api`, `worker`, or combined mode. Operators who want resource isolation
   between interactive API traffic and batch/backtest workloads may run two
   containers of the same image; a single container remains the default and
   the minimum supported footprint.
4. **Scale-down constraint.** Tier-C capability must not raise the minimum
   footprint for tier-A operators: one container plus PostgreSQL, with no
   additional mandatory infrastructure (no queue, no cache, no coordinator).
5. **Data-flow principles for tier C.** Aggregation is pushed down to
   PostgreSQL where scenario semantics allow; evaluation reads stream with
   bounded memory; scenario lookback windows are defined per scenario in
   configuration as time ranges and loaded from the database — never
   accumulated as resident process state.
6. **Determinism rules (Auditability First).** The Go engine must not depend
   on map iteration order; all grouping and output ordering is canonically
   sorted; floating-point evaluation order from the Rust implementation is
   preserved during the port. Identical input must produce identical output
   across runs and across the migration.
7. **Backtest runtime expectations.** Backtests are allowed to run for hours;
   they are scheduled off-peak, and the API returns a simple data-volume-based
   duration estimate that the UI surfaces on the dashboard. Backtest is not a
   latency-bound feature.

## Rationale

- Every defect found in the review is either created by the process boundary
  (message cap, per-customer RPC) or unaffected by language (row-cap
  correctness bug, O(n²) algorithm). Consolidation removes the first class
  outright and gives the engine the database access required to fix the rest.
- ADR-0002's GC concern, quantified, does not hold at the decided targets:
  Go's GC pauses are sub-millisecond, and tier B/C throughput is dominated by
  database I/O and algorithmic complexity, not evaluation-loop speed.
- The recurring cost of the hybrid falls entirely away: one type system
  instead of three, one CI toolchain instead of two, one failure domain
  (engine-unreachable ceases to exist as a failure class, along with its
  health checks, mTLS between internal services, reconnect logic, and gRPC
  metrics surface).
- The one workload where Rust holds a real advantage — a stateful streaming
  engine keeping multi-gigabyte sliding-window state resident in memory,
  where large-heap GC scanning hurts Go — is explicitly excluded by decision
  5 (window state lives in the database). A revisit trigger is defined below
  instead of paying for that future now.
- Marketing value ("technology appeal") is acknowledged as a secondary
  motive for tier-C capability and is recorded as such; the NFRs above are
  justified by the target customers, not by the appeal.

## Alternatives Considered

- **Keep the hybrid and fix the transport in place** (streaming RPCs,
  message-size limits, batched calls): rejected. It repairs the two transport
  defects but retains triple type definitions, dual toolchains, and an engine
  without database access — so streaming evaluation and SQL pushdown would
  require giving the Rust engine its own database integration surface,
  coupling two languages to one schema.
- **Consolidate to Rust**: rejected. It would rewrite ~41k lines of Go API
  (including tests) for no offsetting benefit; ADR-0002's own productivity argument against
  all-Rust still holds.
- **Build the tier-C stateful streaming engine now** (Flink-like, where Rust
  is genuinely advantaged): rejected as premature. Batch and near-line
  evaluation meet the stated requirements; the `interface.go` seam preserves
  the option.

## Consequences

**Positive**

- The 4 MB backtest cap and the per-customer RPC pattern cease to exist
  structurally; in-process calls replace network hops on the interactive
  scoring/screening paths.
- Direct database access enables time-window loading, keyset pagination over
  customers, streaming backtest reads, and SQL aggregation pushdown — the
  actual tier-B/C work.
- CI drops the Rust job (protobuf-compiler, cargo-llvm-cov, cache) and the
  proto lint/breaking job; Dependabot drops the cargo ecosystem; `make`
  targets simplify.
- Operational surface shrinks: no engine health protocol, no API↔engine TLS
  material, no gRPC dashboards. Engine call duration remains observable via
  a new in-process histogram replacing `merlon_grpc_request_duration_seconds`.
- ADR-0012's runtime config digests are computed and exposed by the Go
  binary itself; centralizing engine configuration in the database — deferred
  in ADR-0012 — becomes a feasible follow-up since the engine now shares the
  API's store layer.

**Negative / risks and their mitigations**

- *Loss of process isolation*: a batch or backtest burst competes with
  interactive traffic in one process. Mitigated by the worker mode (decision
  3) and off-peak scheduling (decision 7); the split is an operator choice,
  not a mandatory topology.
- *Migration parity risk*: behavioral drift during the port would silently
  violate Auditability First. Mitigated by a committed golden corpus
  generated from the Rust engine before the port, a CI parity gate, and a
  hard sequencing rule: the Rust crates are deleted only after the gate
  passes. Nondeterministic output fields (time-based alert IDs such as
  `ALT-<millis>-<i>`, `execution_time_ms`, response timestamps) are
  normalized out of comparison, and alert ordering is canonicalized.
- *Go map iteration order is randomized*: a naive port of the engine's
  HashMap grouping would produce run-to-run output reordering. The
  determinism rules (decision 6) make sorted iteration mandatory and the
  parity gate enforces it.
- *Performance ceiling*: Go's evaluation loops are 2–5× slower than Rust's.
  Accepted: at the decided data-flow design the ceiling is I/O-bound, and
  the O(n²)→O(n) scenario fix recovers orders of magnitude more than the
  language delta concedes.
- *Documentation debt*: architecture docs, the adapter guide's gRPC
  references, the published gRPC protocol reference, component CLAUDE.md
  files, compose files, and the PH7 demo composition (which currently
  specifies an Engine container) must all be updated in the same effort.
- *Contract surface*: retiring the proto contract narrows Merlon's external
  contract to the REST API. This is only acceptable pre-publication, which
  constrains migration timing: the consolidation must complete before the
  repository is made public.

**Assumptions recorded**

- Tier-C operators staff alert triage commensurate with their volume; alert
  volume management is addressed by backtesting-driven threshold tuning and
  operator-side rule modeling, not by engine constraints.
- Rule modeling choices (scenario selection, thresholds, windows) are the
  deploying organization's responsibility (Configuration as the Product).

**Revisit trigger**

If a requirement emerges for sustained sub-second, always-on evaluation at
the order of 10⁴+ events/second with resident window state, re-separate a
dedicated streaming engine behind `api/internal/engine/interface.go` and
re-evaluate the implementation language at that time (Rust being a strong
candidate). Until then, the interface seam keeps this decision reversible.

## PH9 implementation notes (2026-07-13)

The first consolidation slice is now present in the repository:

- `api/internal/engine/native` is the in-process implementation of scoring,
  the five TM scenarios, screening, backtest, and config validation. It loads
  the same YAML roots and publishes stable SHA-256 digests.
- `MERLON_MODE=api|worker|all` controls ownership. API mode keeps HTTP,
  realtime/import/notification/retention work; worker mode runs recovery, TM
  batch, and durable backtest jobs; `all` is the one-container default.
- `POST /api/v1/backtests` creates a durable asynchronous job with required UTC
  `[from,to)` bounds, an IDs-or-filter selector, baseline/candidate rule-set
  references, immutable config digests, progress, cancellation, and affected
  customer pagination. Backtests never create alert/case rows. The worker
  resolves non-`active` references through the versioned rule repository and
  runs them in an isolated candidate engine; unresolved or unsupported
  definitions fail closed instead of producing a misleading zero delta. The
  selected customer population is also snapshotted durably at job start.
- The legacy gRPC process was retained only long enough to produce the frozen
  parity corpus. Rust output ordering and all three aggregation windows were
  made deterministic before the golden gate; the transport and crates are now
  removed, leaving native Go as the sole runtime.
- Monetary semantics are intentionally an interim invariant: TM aggregation
  rejects mixed or non-base currencies into `PENDING_REVIEW`; full FX/decimal
  and crypto-asset semantics are a separate PH10 public-release gate.
