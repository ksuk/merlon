# Regulatory Scope

Merlon is designed primarily for AML/CFT operations at Japanese financial institutions, including non-bank financial institutions. Its default retention periods, sample rules, and documentation are examples for that operating context; they are not legal advice or a certification of regulatory compliance.

Deploying organizations are responsible for mapping Merlon to their own obligations, risk assessment, governance, and evidence requirements. This includes additional evaluation for FATF recommendations, EU AMLD and GDPR requirements, the U.S. Bank Secrecy Act, and any other applicable law or supervisory guidance.

## Audit-log integrity boundary

The self-hosted edition supports post-hoc verification through `merlon-audit verify`. It does not provide database-enforced immutability or a cryptographic hash chain for audit logs. Operators must enforce database roles, backups, access controls, and monitoring appropriate to their environment. Database-enforced WORM controls and hash-chain integrity are future enterprise capabilities, not guarantees of this edition.
