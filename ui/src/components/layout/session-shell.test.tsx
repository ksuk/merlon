import { fireEvent, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, expect, test, vi } from "vitest"
import { SessionProvider } from "@/components/session-provider"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { capabilitiesFor } from "@/test/session-test-utils"
import { AppLayout } from "./app-layout"

beforeEach(() => {
  vi.restoreAllMocks()
})

interface ShellStubs {
  me?: unknown
  meStatus?: number
  capabilities?: unknown
  capabilitiesStatus?: number
  onLogout?: () => void
}

function stubShell({ me, meStatus = 200, capabilities, capabilitiesStatus = 200, onLogout }: ShellStubs) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString()

    if (url.includes("/system/capabilities")) {
      return Promise.resolve(
        new Response(capabilitiesStatus === 200 ? JSON.stringify(capabilities) : JSON.stringify({ error: "boom" }), {
          status: capabilitiesStatus,
        }),
      )
    }
    if (url.includes("/auth/logout")) {
      onLogout?.()
      return Promise.resolve(new Response(JSON.stringify({ status: "logged_out" })))
    }
    if (url.includes("/auth/me")) {
      return Promise.resolve(
        new Response(meStatus === 200 ? JSON.stringify(me) : JSON.stringify({ error: "unavailable" }), {
          status: meStatus,
        }),
      )
    }
    if (url.includes("/system/info")) {
      return Promise.resolve(
        new Response(JSON.stringify({ version: "1.0.0", components: [], endpoints: 1, features: {} })),
      )
    }
    void init
    return Promise.resolve(new Response(JSON.stringify({})))
  })
}

function renderShell() {
  return renderWithI18n(
    <MemoryRouter initialEntries={["/"]}>
      <SessionProvider>
        <Routes>
          <Route element={<AppLayout />}>
            <Route index element={<div>page content</div>} />
          </Route>
          <Route path="/login" element={<div>login screen</div>} />
        </Routes>
      </SessionProvider>
    </MemoryRouter>,
  )
}

test("names the signed-in operator and their role", async () => {
  stubShell({
    me: { id: "u1", email: "analyst@example.com", role: "analyst", auth_mode: "session" },
    capabilities: capabilitiesFor({ role: "analyst" }),
  })

  await renderShell()

  expect(await screen.findByText("analyst@example.com")).toBeDefined()
  expect(screen.getByText("アナリスト")).toBeDefined()
})

test("marks an authentication-disabled deployment instead of imitating a session", async () => {
  stubShell({
    meStatus: 503,
    capabilities: capabilitiesFor({ authMode: "disabled" }),
  })

  await renderShell()

  expect(await screen.findByText("認証無効")).toBeDefined()
  // No user directory exists, so the shell says that rather than leaving the
  // identity blank as if it were still loading.
  expect(screen.getByText("ユーザー管理が未設定です")).toBeDefined()
  expect(screen.queryByRole("button", { name: /ログアウト/ })).toBeNull()
})

test("distinguishes an API-key session from a user session", async () => {
  stubShell({
    meStatus: 401,
    capabilities: capabilitiesFor({ authMode: "api_key_only", role: "admin" }),
  })

  await renderShell()

  expect(await screen.findByText("APIキー認証")).toBeDefined()
  expect(screen.getByText("ユーザーセッションなし")).toBeDefined()
})

test("signing out ends the session and returns to the entry state", async () => {
  const logout = vi.fn()
  stubShell({
    me: { id: "u1", email: "admin@example.com", role: "admin", auth_mode: "session" },
    capabilities: capabilitiesFor({ role: "admin" }),
    onLogout: logout,
  })

  await renderShell()

  fireEvent.click(await screen.findByRole("button", { name: /ログアウト/ }))

  await waitFor(() => expect(logout).toHaveBeenCalled())
  expect(await screen.findByText("login screen")).toBeDefined()
})

test("hides navigation for a function this deployment has not configured", async () => {
  stubShell({
    me: { id: "u1", email: "admin@example.com", role: "admin", auth_mode: "session" },
    capabilities: capabilitiesFor({
      role: "admin",
      overrides: { "api_keys.manage": "not_configured", "users.manage": "not_configured" },
    }),
  })

  await renderShell()

  await screen.findByText("admin@example.com")
  expect(screen.queryByText("APIキー")).toBeNull()
  // Webhooks stays configured in this deployment, so it must still be offered:
  // the filter must remove the unconfigured entry, not the whole group.
  expect(screen.getByText("Webhook")).toBeDefined()
})

test("offers user administration once the capability reports it available", async () => {
  stubShell({
    me: { id: "u1", email: "admin@example.com", role: "admin", auth_mode: "session" },
    capabilities: capabilitiesFor({ role: "admin" }),
  })

  await renderShell()

  // The route and page existed before Wave 4, but no navigation reached them.
  expect(await screen.findByText("ユーザー")).toBeDefined()
})

test("keeps navigation visible when the capability contract cannot be read", async () => {
  stubShell({
    me: { id: "u1", email: "admin@example.com", role: "admin", auth_mode: "session" },
    capabilitiesStatus: 500,
  })

  await renderShell()

  await waitFor(() => expect(screen.getByText("page content")).toBeDefined())
  // A failed lookup is not evidence that a function is absent. Removing the
  // menu here would report a transient error as a missing feature.
  expect(screen.getByText("APIキー")).toBeDefined()
})
