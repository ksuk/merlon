---
title: FSA Guideline Mapping
---

# FSA Guideline Mapping

This document maps major control areas in the Financial Services Agency's
(FSA) [Guidelines for Anti-Money Laundering and Combating the Financing of
Terrorism](https://www.fsa.go.jp/en/news/2026/20260331/20260331.html), revised
March 31, 2026, to Merlon capabilities and deployment responsibilities. It is
an implementation aid, not a legal opinion or certification.

## Status Definitions

- **Implemented capability**: Merlon provides the stated technical function.
  The deploying institution must still configure, operate, and validate it.
- **Partial**: Merlon supports part of the control; a source system, human
  decision, external service, or documented operating procedure is required.
- **Operator-controlled**: Merlon can preserve evidence or consume inputs, but
  the control itself is owned by the deploying institution.

## Mapping Table

Last implementation review: **July 16, 2026**.

| Guideline area | Status | Merlon capability and repository evidence | Limitation and deploying-institution responsibility |
|---|---|---|---|
| Enterprise risk assessment and risk-based approach | Implemented capability | Score-Driven Architecture, configurable CDD weights and tier thresholds, and score history (`docs/architecture.md`, `content/schema/cdd_weight_v1.json`) | Define the institution's risk appetite, validate factors and thresholds against its own risk assessment, approve changes, and review effectiveness. Sample content is not a production control. |
| Customer due diligence and risk rating | Partial | Customer records, CDD scoring, risk tiers, status history, and enhanced-due-diligence escalation (`api/internal/engine/native`, `api/internal/server/customer_status.go`) | Identity verification, beneficial-owner checks, source-of-funds evidence, and authoritative customer data must come from institutional procedures or integrated systems. |
| Ongoing customer management | Partial | Scheduled re-screening, batch evaluation, tier-change events, alerts, cases, and customer review evidence (`api/internal/screening/scheduler.go`, `api/internal/batch`) | Set review frequency, keep source data current, resolve alerts, document enhanced review, and test that schedules match customer risk. |
| Transaction monitoring and analysis | Implemented capability | Native TM scenarios, inline and batch evaluation, aggregation windows, alerts, cases, recovery paths, and backtesting (`api/internal/engine/native`, `api/internal/batch`) | Select and calibrate scenarios, investigate alerts, measure false positives and missed risk, and document periodic effectiveness testing. |
| Sanctions and watchlist screening | Partial | Screening adapters, list import, version snapshots, freshness checks, re-screening, and match review (`api/internal/screening`) | Merlon is not an official-list distributor. Acquire complete and timely lists, validate import coverage, define match disposition, and handle any legally required asset-freeze or reporting action. |
| Suspicious transaction reporting | Partial | Alert-to-case workflow, investigation notes, case status, STR records, and export support (`docs/case-management.md`, `api/internal/server/case.go`) | A qualified officer decides whether to file, confirms the required format and deadline, approves the report, and submits it through the competent authority's channel. |
| Records, audit trail, and retention | Implemented capability with operational controls | Append-only audit events, atomic rule-approval events, configurable retention, purge controls, and audit privilege verification (`docs/compliance/data-retention.md`, `docs/operations/audit-hardening.sql`) | Apply least-privilege database roles, protect encryption keys and backups, operate legal holds where required, perform restore tests, and retain evidence for the legally required period. |

## Evidence and Review Rules

The paths above identify source-controlled implementation evidence. Runtime
evidence must additionally include the deployed release digest, engine
configuration digests, rule versions and approvals, audit records, CI results,
and operating records for the relevant period. Re-review this mapping after a
guideline revision or any material feature, configuration, or operating-model
change.

This mapping does not guarantee that a deploying institution's AML/CFT program
conforms to FSA guidelines or other applicable law. The institution must obtain
qualified legal and compliance review and remains accountable for final
conformance, governance, staffing, data quality, and operation.
