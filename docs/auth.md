---
sidebar_position: 7
title: Authorization and Segregation of Duties
---

# Authorization and Segregation of Duties

Merlon authenticates interactive users with JWT sessions and non-interactive
clients with API keys. Both paths apply the same role model. Deployments must
map these roles to their own access-control and change-management policy.

| Permission | Admin | Analyst | Viewer |
|---|:---:|:---:|:---:|
| Request a whitelist entry (`whitelist:request`) | Yes | Yes | No |
| Approve a whitelist entry (`whitelist:approve`) | Yes | No | No |
| Read audit records (`audit:read`) | Yes | No | No |
| Create, update, import, activate, or deactivate rules (`rule:write`) | Yes | No | No |

The whitelist workflow enforces that a requester cannot approve their own
request. This provides a separate first-line request and second-line approval
control for whitelist decisions.

Rule activation and deactivation enforce the same separation in the
application, inside the repository transaction rather than as a pre-check
(ADR-0014). The exact target version is locked, and the request is rejected
with `403` when the approver and the creator of that version are the same
identity, or when either identity is missing — the control fails closed rather
than guessing. Rules and rule versions created through the API are always
inactive, so activation is always a second, separately attributable act.

Each state change writes a `rule_activation_events` row in the same
transaction, recording the target version, creator, approver, requested state,
and timestamp. The serving role holds only `SELECT` and `INSERT` on that
ledger, so the approval evidence cannot be altered after the fact.

For authentication design and credential guidance, see
[Configuration Reference](configuration.md).
