# Report Template

Output path: `.audit/repo-review-<YYYYMMDD>-<shortsha>.md` (never commit).
Follow the skeleton below. The Standard's Appendix A (findings) and Appendix D
(score sheet) are the authoritative definitions for each format.

```markdown
# Repository Quality Audit Report

| Field | Value |
|---|---|
| Repository / target revision | <name> / <full sha> |
| Standard version | Repository Quality Review Standard v<x.y.z> |
| Profile | oss / internal / regulated |
| Executed at / by | <ISO8601> / <agent + model> |
| Available evidence classes | E1 / E2 / (E3) — no E4 |
| Evidence storage path | .audit/evidence-<...>/ |

## 1. Overall Rating

- Overall score: <0.0-4.0> / Rank: <A|B|C|D|Non-compliant>
- Gating triggered: <none | details>
- Finding counts: Blocker n / Critical n / Major n / Minor n / Info n (of which n Unconfirmed)

## 2. Domain Summary

| Domain | Score | Cap applied | Key rationale (1 line) |
|---|---|---|---|
| D1–D12 (applicable domains only) | | | |

List 2–3 strengths and 2–3 weaknesses as bullets.

## 3. Findings (by severity)

Each finding uses the Appendix A fields:

### F-<YYYY>-<NNN>: <one-line summary>
- Item: Dn-m / Severity: <> / Confidence: <> / Status: Open
- Evidence: <path:line> (E1), <commit> (E2), executed: `<cmd>` → <result summary>
- Impact: <failure scenario>
- Recommendation: <direction, not a prescribed implementation>
- Due: <default from severity, or reason for adjustment>

## 4. Good Practices (Info)

## 5. NE (Not Evaluable) Items and Human Follow-up

| Item | Reason for NE | What's needed (E3/E4, etc.) |
|---|---|---|

## 6. Tailoring Log

| Target | Adjustment | Reason |
|---|---|---|

## 7. Methodology Notes

- Sampling: method / sample size / population (for each item where sampling was used)
- Collectors run, with version info
- Comparison against the previous audit (recurring runs only): score trend / new findings / resolved findings
```
