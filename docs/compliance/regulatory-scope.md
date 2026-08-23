---
title: Regulatory Scope
---

# Regulatory Scope

Merlon is designed primarily for AML/CFT operations at Japanese financial institutions, including non-bank financial institutions. Its default retention periods, sample rules, and documentation are examples for that operating context; they are not legal advice or a certification of regulatory compliance.

Deploying organizations are responsible for mapping Merlon to their own obligations, risk assessment, governance, and evidence requirements. This includes additional evaluation for FATF recommendations, EU AMLD and GDPR requirements, the U.S. Bank Secrecy Act, and any other applicable law or supervisory guidance.

## Legal holds

Merlon has no per-record legal-hold mechanism. Preservation
obligations that outlast the configured retention periods must be handled
operationally by the deploying organization; see
[Data Retention Policy](data-retention.md#legal-holds) for the procedure and
its limits.

## Audit-log integrity boundary

Merlon supports post-hoc verification through `merlon-audit verify`. It does not provide database-enforced immutability, database-enforced WORM controls, or a cryptographic hash chain for audit logs. Operators must enforce database roles, backups, access controls, and monitoring appropriate to their environment.
