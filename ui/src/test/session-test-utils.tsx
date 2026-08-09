import type { ReactElement } from "react"
import { MemoryRouter } from "react-router"
import { SessionProvider } from "@/components/session-provider"
import type { AuthMode, Capabilities, CapabilityDescriptor, Role } from "@/lib/api"

/**
 * Helpers for exercising pages that read the capability contract.
 *
 * A page rendered without a SessionProvider sees no capability at all, so
 * every privileged control is withheld. That is the correct production
 * behaviour and a misleading test setup, which is why page tests wrap with
 * `renderInSession` and stub `/system/capabilities` with `capabilitiesFor`.
 */

const permissionsByRole: Record<Role, string[]> = {
  admin: [
    "audit:read",
    "batch:execute:large",
    "cdd:override:approve",
    "cdd:score",
    "rule:write",
    "whitelist:approve",
    "whitelist:request",
  ],
  analyst: ["cdd:score", "whitelist:request"],
  viewer: [],
}

// capabilityPermissions mirrors api/internal/server/capability.go. A test that
// disagreed with the server's catalog would prove nothing, so the mapping is
// stated once here and reused.
const capabilityPermissions: Record<string, string | undefined> = {
  "api_keys.manage": undefined,
  "users.manage": undefined,
  "webhooks.manage": undefined,
  "retention.manage": undefined,
  "accounts.manage": undefined,
  "screening.review": undefined,
  "rules.write": "rule:write",
  "config.validate": undefined,
  "whitelist.request": "whitelist:request",
  "whitelist.approve": "whitelist:approve",
  "audit.read": "audit:read",
  "cdd.score": "cdd:score",
  "cdd.override.approve": "cdd:override:approve",
  "batch.execute.large": "batch:execute:large",
}

export interface CapabilityStubOptions {
  role?: Role
  authMode?: AuthMode
  /** Capability ids to force to a non-available state. */
  overrides?: Record<string, CapabilityDescriptor["availability"]>
}

/** Builds a GET /system/capabilities body for the given role. */
export function capabilitiesFor(options: CapabilityStubOptions = {}): Capabilities {
  const { role = "admin", authMode = "session", overrides = {} } = options
  const held = permissionsByRole[role] ?? []
  const checkedAt = "2026-08-08T00:00:00Z"

  const data: CapabilityDescriptor[] = Object.entries(capabilityPermissions).map(
    ([id, permission]) => {
      const forced = overrides[id]
      const permitted =
        authMode === "disabled" || !permission || held.includes(permission)
      return {
        id,
        availability: forced ?? (permitted ? "available" : "forbidden"),
        required_permission: permission,
        surfaces: ["ui", "api"],
        reason_code: forced ?? permitted ? undefined : "permission_required",
        checked_at: checkedAt,
      }
    },
  )

  return {
    auth_mode: authMode,
    role: authMode === "disabled" ? undefined : role,
    permissions: authMode === "disabled" ? [] : held,
    checked_at: checkedAt,
    data,
  }
}

/** Wraps a page in the router and session context the shell provides. */
export function inSession(ui: ReactElement): ReactElement {
  return (
    <MemoryRouter>
      <SessionProvider>{ui}</SessionProvider>
    </MemoryRouter>
  )
}
