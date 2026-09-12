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

After setup, an Admin manages local accounts through **User management** or the
`/api/v1/admin/users` API:

| Action | Endpoint |
|---|---|
| List accounts | `GET /api/v1/admin/users` |
| Create an account | `POST /api/v1/admin/users` |
| Change role or active state | `PATCH /api/v1/admin/users/{id}` |
| Set a replacement password | `POST /api/v1/admin/users/{id}/reset-password` |
| Force all sessions to end | `POST /api/v1/admin/users/{id}/revoke-sessions` |

Passwords must contain at least 12 characters. Role and active-state changes,
password resets, and explicit session revocation end every session that was
active for the affected account. A later login starts a new session under the
current role. An inactive account cannot log in. The last active Admin cannot
be disabled or demoted.

Account mutations and their audit entry commit together. Audit details include
authority changes but never passwords, password hashes, tokens, or refresh-
session family identifiers.

The consequence for deployment is that the window between first start and first
administrator is the one moment where an unauthenticated caller can create a
privileged account. Do not expose a fresh instance to an untrusted network
before completing setup.

`MERLON_AUTH_ENABLED=false` disables authentication entirely. It exists for the
demo topology and for local development. It must never be set in production;
see [Configuration Reference](configuration.md).

## Session lifecycle

Each successful login starts an independent refresh-token family and issues an
access token with both a token identifier and that family identifier. Refresh
rotates the refresh token within the same family. Reuse of a rotated token
revokes the family as a compromise signal.

Logout revokes the current access token and refresh-token family. Other
concurrent sessions for the same user remain active, and an immediate new login
starts a different family that is not affected by the logout. A user can have
up to five active families; starting another session evicts the oldest one.
In PostgreSQL deployments, each authenticated request verifies that the
persisted refresh-token family remains active. Logout and forced revocation are
therefore visible across API replicas and survive an API process restart.
Access tokens issued by an older build without a session-family identifier are
rejected after the upgrade; affected users must log in again.
Each refresh-token family also records the role that started the session. If an
administrator changes or disables that user, existing access tokens and refresh
tokens are rejected; the user must log in again under the new authority.
Refresh and logout are state-changing cookie-authenticated requests. Clients
must echo the `csrf_token` cookie in the `X-CSRF-Token` header. A missing or
mismatched token is rejected with `403` before session state changes.
If persistent server-side revocation cannot be confirmed, the response is an
error rather than a successful logout; browser cookies are still cleared and
the failed attempt is audited. A failure in the process-local revocation cache
does not override a revocation already committed to PostgreSQL.

An Admin can call
`POST /api/v1/admin/users/{id}/revoke-sessions` to invalidate all currently
active families for one user. This is the user-wide path for an intentional
forced logout. It does not deny a later successful login. Login success,
refresh, logout, current-session revocation, and user-wide revocation are
recorded as distinct audit actions; token values and refresh-family identifiers
are not written to the audit log.

## Roles

| Permission | Admin | Analyst | Viewer |
|---|:---:|:---:|:---:|
| Request a whitelist entry (`whitelist:request`) | Yes | Yes | No |
| Approve a whitelist entry (`whitelist:approve`) | Yes | No | No |
| Read audit records (`audit:read`) | Yes | No | No |
| Manage local users (`user:manage`) | Yes | No | No |
| Create, update, import, activate, or deactivate rules (`rule:write`) | Yes | No | No |
| Confirm a bulk run covering a large target population (`batch:execute:large`) | Yes | No | No |
| Re-score a customer (`cdd:score`) | Yes | Yes | No |
| Approve a proposed risk-tier override (`cdd:override:approve`) | Yes | No | No |

This table is the whole grant model. `GET /api/v1/system/capabilities` derives
its answers from it rather than from a second, separately maintained matrix, so
a permission added here reaches the operator interface without further wiring.

## Capability discovery

`GET /api/v1/system/capabilities` reports, for each administrative or
permission-gated function, whether it is `available`, `not_configured`,
`forbidden`, `unsupported`, `degraded` or `unavailable`, which permission it
needs, whether it is offered as a screen or only as an endpoint, and where it is
documented. `GET /api/v1/auth/me` carries the same session's `auth_mode`,
`roles` and `permissions`.

The interface uses this to hide or explain a control. It is not authorization:
every capability named there is enforced again on the route that performs the
action. A control that is hidden by the client is still refused by the server.

`auth_mode` distinguishes three deployments:

| `auth_mode` | Meaning |
|---|---|
| `session` | Users log in; API keys also work |
| `api_key_only` | API keys authenticate, but no JWT signing key is configured, so no user can log in |
| `disabled` | `MERLON_AUTH_ENABLED=false`: no role reaches any request |

With authentication disabled every capability whose dependency is configured
reports `available`, because no role exists that could refuse one. A function
this deployment never configured still reports `not_configured` — an absent
permission and an absent feature are different facts (ADR-0024).

### Functions supported through the API only

Not every backend capability has a screen, and an intentional omission is
recorded rather than left to be discovered:

| Capability | Endpoints | Why there is no screen |
|---|---|---|
| `retention.manage` | `GET`/`PUT /api/v1/admin/retention-policies` | Retention periods are set once per deployment by an administrator and are audited; see [Retention and purge](compliance/data-retention.md) |
| `accounts.manage` | `POST /api/v1/accounts`, `GET /api/v1/accounts/{id}`, `POST`/`GET /api/v1/accounts/{id}/customers` | Account linkage is established at ingestion by the adapter layer, not by an analyst; see [Adapter guide](adapter-guide.md) |

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
