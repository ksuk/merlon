---
sidebar_position: 7
title: Authorization and Segregation of Duties
---

# Authorization and Segregation of Duties

Merlon authenticates interactive users with JWT sessions and non-interactive
clients with API keys. Both paths apply the same role model. Deployments must
map these roles to their own access-control and change-management policy.

## The first administrator

A new deployment has no accounts, so no login credential exists.
`POST /api/v1/setup`, reached in the browser at `/setup`, creates the sole
initial Admin account and nothing else.

- It is reachable without authentication, because at that moment there is no
  credential that could authenticate anyone.
- It succeeds exactly once. Once any user row exists it returns `409`, so it
  cannot be replayed to mint a second administrator later.
- The password minimum is 12 characters.
- The login screen links to it, since a first-run operator would otherwise have
  to know the URL. The link is unconditional: probing the server for whether
  any account exists would disclose that to anyone who can reach the login
  page.

The current release has no supported API or UI flow for creating subsequent
accounts. **User management** and `GET /api/v1/admin/users` provide a read-only
list of existing users; they do not create users.

The consequence for deployment is that the window between first start and first
administrator is the one moment where an unauthenticated caller can create a
privileged account. Do not expose a fresh instance to an untrusted network
before completing setup.

`MERLON_AUTH_ENABLED=false` disables authentication entirely. It exists for the
demo topology and for local development. It must never be set in production;
see [Configuration Reference](configuration.md).

## Roles

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
