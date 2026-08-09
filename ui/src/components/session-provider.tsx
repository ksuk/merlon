import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react"
import { useNavigate } from "react-router"
import { ApiError, api, type AuthUser, type Capabilities, type CapabilityDescriptor } from "@/lib/api"
import { SessionContext, type SessionValue, type UserState } from "@/lib/session-context"

interface ProfileResult {
  user: AuthUser | null
  state: UserState
}

// readProfile treats the two documented "no user here" answers as states
// rather than errors. GET /auth/me returns 503 when user management is not
// configured (an authentication-disabled deployment) and 401 when the caller
// authenticated with an API key instead of a session. Reporting either as a
// failure would tell an operator something is broken when nothing is.
async function readProfile(): Promise<ProfileResult> {
  try {
    return { user: await api.auth.me(), state: "identified" }
  } catch (error) {
    if (error instanceof ApiError && error.status === 503) {
      return { user: null, state: "not_configured" }
    }
    if (error instanceof ApiError && error.status === 401) {
      return { user: null, state: "unauthenticated" }
    }
    return { user: null, state: "failed" }
  }
}

/**
 * SessionProvider is the single reader of the current session and the
 * server-sourced capability contract.
 *
 * Before this existed, three pages each issued their own uncached GET
 * /auth/me and derived privileged controls from the role inline, so the same
 * question was asked three times and answered three different ways. Capability
 * state now has one owner, and every consumer sees the same answer.
 *
 * Visibility here is an affordance, never authorization: the server enforces
 * the same permission on the route that performs the action.
 */
export function SessionProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const [attempt, setAttempt] = useState(0)
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null)
  const [capabilityError, setCapabilityError] = useState<string | null>(null)
  const [user, setUser] = useState<AuthUser | null>(null)
  const [userState, setUserState] = useState<UserState>("loading")
  const [loading, setLoading] = useState(true)
  const [loggingOut, setLoggingOut] = useState(false)
  const [logoutError, setLogoutError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    void (async () => {
      setLoading(true)
      setUserState("loading")

      const [capabilityResult, profile] = await Promise.all([
        api.system
          .capabilities()
          .then((value) =>
            // A body without a descriptor array is not a contract. Storing it
            // would make every later lookup throw inside a render and take the
            // shell down, which is precisely the containment failure #85
            // describes.
            Array.isArray(value?.data)
              ? { value, error: null as string | null }
              : { value: null, error: "malformed capability contract" },
          )
          .catch((error: unknown) => ({
            value: null,
            error: error instanceof Error ? error.message : String(error),
          })),
        readProfile(),
      ])

      if (cancelled) return

      setCapabilities(capabilityResult.value)
      setCapabilityError(capabilityResult.error)
      setUser(profile.user)
      setUserState(profile.state)
      setLoading(false)
    })()

    return () => {
      cancelled = true
    }
  }, [attempt])

  const refresh = useCallback(() => setAttempt((value) => value + 1), [])

  const logout = useCallback(async () => {
    setLoggingOut(true)
    setLogoutError(null)
    try {
      await api.auth.logout()
    } catch (error: unknown) {
      // The server clears cookies and revokes the refresh family on its own
      // path; if the call failed we cannot know whether it ran. Returning the
      // operator to the entry state is still the safe outcome, so the failure
      // is reported rather than used to keep them in an ambiguous session.
      setLogoutError(error instanceof Error ? error.message : String(error))
    } finally {
      setLoggingOut(false)
      setUser(null)
      setUserState("unauthenticated")
      navigate("/login", { replace: true })
    }
  }, [navigate])

  const capabilityFor = useCallback(
    (id: string): CapabilityDescriptor | null => {
      if (!capabilities) return null
      return capabilities.data.find((descriptor) => descriptor.id === id) ?? null
    },
    [capabilities],
  )

  const value = useMemo<SessionValue>(
    () => ({
      user,
      userState,
      authMode: capabilities?.auth_mode ?? user?.auth_mode ?? null,
      capabilities,
      loading,
      capabilityError,
      refresh,
      logout,
      logoutError,
      loggingOut,
      capabilityFor,
    }),
    [user, userState, capabilities, loading, capabilityError, refresh, logout, logoutError, loggingOut, capabilityFor],
  )

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}
