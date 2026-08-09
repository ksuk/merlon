import { useContext } from "react"
import type { CapabilityDescriptor } from "@/lib/api"
import { SessionContext, reasonCapabilityLookupFailed, type SessionValue } from "@/lib/session-context"

/**
 * useSession reads the current session and capability contract.
 *
 * Outside a SessionProvider it returns a resolved "nothing is known" value
 * rather than throwing. A page rendered in isolation — a test, a Storybook
 * entry, the login screen — should degrade to showing no privileged control,
 * not crash the shell it is trying to render.
 */
export function useSession(): SessionValue {
  const value = useContext(SessionContext)
  if (value) return value

  return {
    user: null,
    userState: "loading",
    authMode: null,
    capabilities: null,
    loading: false,
    capabilityError: null,
    refresh: () => {},
    logout: async () => {},
    logoutError: null,
    loggingOut: false,
    capabilityFor: () => null,
  }
}

/**
 * useCapability resolves one capability, never returning null.
 *
 * A capability the server never described and a capability we failed to ask
 * about are different facts, and the UI has to say which one it is: the first
 * is a deployment that does not offer the function, the second is a lookup the
 * operator can retry. Both are reported as unavailable-with-a-reason rather
 * than as an absent control.
 */
export function useCapability(id: string): CapabilityDescriptor {
  const { capabilities, capabilityError, capabilityFor } = useSession()

  const found = capabilityFor(id)
  if (found) return found

  return {
    id,
    availability: "unavailable",
    surfaces: ["ui"],
    reason_code: capabilities === null && capabilityError !== null ? reasonCapabilityLookupFailed : "unknown_capability",
    checked_at: capabilities?.checked_at ?? new Date(0).toISOString(),
  }
}

/**
 * useCan is the affordance test: may this operator be shown the control?
 * It is deliberately narrow — only "available" qualifies, so a degraded or
 * unknown capability renders its explanation instead of an action that will
 * fail.
 */
export function useCan(id: string): boolean {
  return useCapability(id).availability === "available"
}
