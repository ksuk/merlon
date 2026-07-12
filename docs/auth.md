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

Rule activation currently has no in-application creator-versus-activator
separation. Organizations must enforce that separation with IAM and their
change-management process until a product control is introduced. See
ADR-0012.

For authentication design and credential guidance, see
[Configuration Reference](configuration.md).
