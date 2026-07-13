---
name: repo-review
description: >-
  Runs a repository quality audit against the internal Repository Quality Review
  Standard (docs/standards/repository-quality-review-standard.md, written in
  Japanese) and produces an audit report with findings (Appendix A format) and a
  score sheet (Appendix D format). Use when asked to audit or review repository
  quality against the internal standard (e.g. "audit the repo", "run a quality
  review", "リポジトリ監査", "品質監査を実施", "/repo-review").
  Args: "[profile] [domains] [--delegate]" — profile: oss|internal|regulated,
  domains: comma-separated subset like D1,D6,D7 (default: all), --delegate: evaluate
  domains via Sonnet subagents with main-agent acceptance.
---

# Repository Quality Audit (/repo-review)

This skill is the execution procedure for producing an audit **instance derived
from** the Repository Quality Review Standard (`docs/standards/repository-quality-review-standard.md`,
hereafter "the Standard" — written in Japanese; see section 1.4 of the Standard for
why derived checklists, not the framework itself, are what gets executed). All
evaluation semantics — evaluation items, maturity criteria, severity, AI-agent
judgment rules — live in the Standard. This skill defines only the execution
skeleton.

## Arguments

| Argument | Values | Default |
|---|---|---|
| profile | `oss` / `internal` / `regulated` | If unspecified, confirm via AskUserQuestion (recommended default for this repository: `internal`; use `regulated` only when evaluating against the adopting organization's regulatory context) |
| domains | Comma-separated, e.g. `D1,D6,D7` | All domains (excluding any the profile weights to 0) |
| `--delegate` | Flag | Delegate domain evaluation to Sonnet subagents; the main agent performs acceptance review only |

## Procedure

### 0. Read the Standard first (do not skip)

Read `docs/standards/repository-quality-review-standard.md` in full before evaluating.
Evaluating from memory or a prior summary is prohibited. Chapter 7 of Part I (AI
Agent Operating Protocol) constrains every step below.

### 1. Scoping

1. Pin the target revision: `git rev-parse HEAD`. If `git status --porcelain` is
   non-empty, confirm with the user whether to include the dirty working tree in
   scope (default: audit HEAD only, and record that choice).
2. If `git rev-parse --is-shallow-repository` is `true`, history-dependent
   judgments are NE (Standard section 7.5).
3. Confirm the profile and apply the standard tailoring table (Standard chapter 8).
   Record every applied tailoring decision in the report's tailoring log.
4. Determine available evidence classes: E1/E2 are always available. E3 (platform
   records — PR reviews, branch protection) requires reachability, e.g.
   `gh api repos/{owner}/{repo} --jq .full_name`; if unreachable, items requiring
   E3 are NE. E4 (interviews) is out of scope for this skill — list E4-dependent
   items under "items requiring human follow-up" in the report.

### 2. Evidence collection

Run the read-only command set in `references/collectors.md` and save raw output
under `.audit/evidence-<YYYYMMDD>-<shortsha>/`. Collection must be read-only: do
not run commands that mutate repository state, and do not make outbound network
calls (e.g. EOL lookups) — if a lookup can't be made, mark the dependent judgment
NE.

### 3. Domain-by-domain evaluation

For each domain, evaluate in the order given in the Standard's Part II: "How to
verify" first, then "AI agent judgment criteria":

- Run **machine-checkable signals first** (reusing collector output), then perform
  judgment-based evaluation.
- For each evaluation item, record a maturity level (L0–L4, half-steps allowed)
  with rationale. Mark undeterminable items NE with a reason.
- When sampling, follow Standard section 7.4: record the sampling method, sample
  size, and population size.
- Don't chase completeness past the point of diminishing returns on large
  domains — flagging an open question as "needs human follow-up" is worth more
  than an unsupported assertion.

**Delegation (`--delegate`)**: split domains into 3–4 bundles (e.g. D1+D2+D10 /
D3+D4+D11 / D5+D8 / D6+D7+D9+D12) and launch one Sonnet subagent per bundle. Each
briefing must include: the pinned revision / the assigned domains (instruct the
subagent to read the corresponding sections of the Standard) / the collector
output path / the output contract (per-item ratings plus Appendix-A-format
findings, evidence references mandatory) / the constraints in Standard chapter 7.
**Acceptance review**: the main agent independently re-runs or re-checks the cited
evidence (path, line, command) for every finding and discards any finding it
cannot verify.

### 4. Finalizing findings (applying Standard chapter 7)

- Record findings using the Appendix A fields. **Re-confirm evidence exists**
  before writing it into the report.
- Findings at Critical or above without High-confidence evidence must be flagged
  "Unconfirmed."
- Never transcribe secret-like values into output (path, commit ID, and category
  only). Never actively verify a credential (e.g. by attempting authentication).
- Always include good practices (Info-level).
- When reporting author statistics, describe them as structural information
  (roles, concentration) rather than by individual name (Standard section 1.3).

### 5. Scoring

Follow Standard chapter 4: per-item average → severity cap → profile weighting
(Standard section 4.2 table) → overall score → gating → rank. Exclude NE items
from the denominator; if NE items exceed half of a domain's items, mark that
domain "not evaluable."

### 6. Report output

1. Write the report to `.audit/repo-review-<YYYYMMDD>-<shortsha>.md` using the
   structure in `references/report-template.md` (`.audit/` is gitignored — never
   commit reports).
2. Summarize for the user: overall rating, domain highlights (2–3 strengths, 2–3
   weaknesses), any Blocker/Critical findings, and the count of items needing
   human follow-up; send the report file to the user.
3. If a Blocker is found, report it immediately — interrupt the remaining
   procedure rather than waiting until the end (Standard chapter 6, step 4).

## Constraints (summary)

- The audit is **evaluation only**. Do not implement fixes or commit changes —
  stop at findings and recommendations.
- Never repurpose this skill's output for individual performance evaluation
  (Standard section 1.3).
- Always include the target revision, execution timestamp, and the Standard's
  version (see its revision history) in the output.

## Recurring runs

For a monthly cadence, register `/repo-review internal --delegate` via
`/schedule`. On recurring runs, include a diff against the previous report
(score trend, new/resolved findings) in the summary.
