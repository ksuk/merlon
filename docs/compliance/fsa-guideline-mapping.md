---
title: FSA Guideline Mapping
---

# FSA Guideline Mapping

This document maps key items in the Financial Services Agency's (FSA)
"Guidelines for Anti-Money Laundering and Combatting the Financing of
Terrorism" to the corresponding Merlon capabilities.

## Purpose

To let a deploying institution's compliance staff see which Merlon
capability addresses each FSA guideline requirement, and to help separate
what the system covers from what must be covered by operational process when
building out an AML/CFT program.

## Mapping table

| FSA guideline item | Merlon capability | Status |
|---|---|---|
| Customer due diligence at transaction time | CDD scoring | TODO: expand detailed coverage in M1.3 |
| Filing suspicious transaction reports | TM + case management + STR export | TODO: expand STR format support in M2.x |
| Ongoing customer management | CDD scoring (periodic review) | TODO: expand the periodic review workflow |
| Risk-based approach | Score-Driven Architecture | TODO: expand risk-rating operational guidance |
| Transaction monitoring | TM engine | M2.1: structuring detection and rapid fund movement detection scenarios implemented |
| Sanctions-list screening | Screening engine | TODO: expand list integrations |
| Record retention | Audit logs + data retention policy | TODO: expand retention period configuration detail |

## Notes

This document is a **skeleton at the M1.1 stage**. Detailed coverage for
each item — concrete feature specifications, operational procedures, and
evidence output — will be filled in incrementally in future milestones.

This mapping table does not guarantee that a deploying institution's AML/CFT
program conforms to the FSA guidelines. The deploying institution and its
advisors remain responsible for the final conformance determination.
