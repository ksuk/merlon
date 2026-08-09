import { createContext } from "react"
import type { AuthMode, AuthUser, Capabilities, CapabilityDescriptor } from "@/lib/api"

// UserState separates the reasons the shell may have no identity to show.
// "unauthenticated" and "not_configured" are normal operating states, not
// failures: an API-key session has no user, and an authentication-disabled
// deployment has no user directory at all. Collapsing them into one empty
// value is what let a page present a missing control as a product limitation
// (#81).
export type UserState =
  | "loading"
  | "identified"
  | "not_configured"
  | "unauthenticated"
  | "failed"

export interface SessionValue {
  /** The authenticated user, or null in every state except "identified". */
  user: AuthUser | null
  userState: UserState
  /** How this deployment authenticates callers; null while still loading. */
  authMode: AuthMode | null
  capabilities: Capabilities | null
  /** True while either the capability contract or the profile is in flight. */
  loading: boolean
  /**
   * Set when the capability contract itself could not be read. The UI must
   * then report a lookup failure rather than silently render as if every
   * control were forbidden.
   */
  capabilityError: string | null
  /** Re-reads the capability contract and the profile. */
  refresh: () => void
  logout: () => Promise<void>
  logoutError: string | null
  loggingOut: boolean
  capabilityFor: (id: string) => CapabilityDescriptor | null
}

// reasonCapabilityLookupFailed is a client-side reason code. It is deliberately
// distinct from the server's reason codes so an operator can tell "the server
// says you may not" from "we could not ask".
export const reasonCapabilityLookupFailed = "capability_lookup_failed"

export const SessionContext = createContext<SessionValue | null>(null)
