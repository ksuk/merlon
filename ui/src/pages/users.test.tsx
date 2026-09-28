import { fireEvent, screen, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { SessionContext } from "@/lib/session-context"
import type { Capabilities, CapabilityDescriptor } from "@/lib/api"
import { UsersPage } from "./users"

function renderWithRouter(ui: React.ReactElement) {
  const descriptor: CapabilityDescriptor = {
    id: "users.manage",
    availability: "available",
    required_permission: "user:manage",
    surfaces: ["ui", "api"],
    checked_at: "2026-09-12T00:00:00Z",
  }
  const capabilities: Capabilities = {
    auth_mode: "session",
    role: "admin",
    permissions: ["user:manage"],
    checked_at: descriptor.checked_at,
    data: [descriptor],
  }
  return renderWithI18n(
    <MemoryRouter>
      <SessionContext.Provider value={{
        user: { id: "admin", email: "admin@example.com", role: "admin", auth_mode: "session" },
        userState: "identified",
        authMode: "session",
        capabilities,
        loading: false,
        capabilityError: null,
        refresh: () => {},
        logout: async () => {},
        logoutError: null,
        loggingOut: false,
        capabilityFor: (id) => id === descriptor.id ? descriptor : null,
      }}>
        {ui}
      </SessionContext.Provider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders user list with role badges", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          id: "u1",
          email: "alice@example.com",
          role: "admin",
          active: true,
          created_at: "2025-01-15T10:00:00Z",
          updated_at: "2025-01-15T10:00:00Z",
        },
      ]),
    ),
  )

  await renderWithRouter(<UsersPage />)

  expect(await screen.findByText("alice@example.com")).toBeDefined()
  expect(screen.getAllByText("管理者").length).toBeGreaterThan(0)
})

test("shows empty state", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  await renderWithRouter(<UsersPage />)

  expect(await screen.findByText("ユーザが登録されていません")).toBeDefined()
})

test("creates a local user and refreshes the list", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch")
  fetchMock
    .mockResolvedValueOnce(new Response(JSON.stringify([])))
    .mockResolvedValueOnce(new Response(JSON.stringify({ id: "u2", email: "viewer@example.com", role: "viewer", active: true, created_at: "2026-09-11T00:00:00Z", updated_at: "2026-09-11T00:00:00Z" }), { status: 201 }))
    .mockResolvedValueOnce(new Response(JSON.stringify([{ id: "u2", email: "viewer@example.com", role: "viewer", active: true, created_at: "2026-09-11T00:00:00Z", updated_at: "2026-09-11T00:00:00Z" }])))

  await renderWithRouter(<UsersPage />)
  fireEvent.click(await screen.findByRole("button", { name: "ユーザを作成" }))
  fireEvent.change(screen.getByLabelText("メールアドレス"), { target: { value: "viewer@example.com" } })
  fireEvent.change(screen.getByLabelText("初期パスワード"), { target: { value: "correct-horse-battery-staple" } })
  fireEvent.change(screen.getByLabelText("ロール"), { target: { value: "viewer" } })
  fireEvent.click(screen.getByRole("button", { name: "アカウントを作成" }))

  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
  const [, init] = fetchMock.mock.calls[1]
  expect(init?.method).toBe("POST")
  expect(JSON.parse(String(init?.body))).toEqual({ email: "viewer@example.com", password: "correct-horse-battery-staple", role: "viewer" })
  expect(await screen.findByText("viewer@example.com")).toBeDefined()
})

test("updates role and active state through an explicit save", async () => {
  const user = { id: "u1", email: "analyst@example.com", role: "analyst", active: true, created_at: "2026-09-11T00:00:00Z", updated_at: "2026-09-11T00:00:00Z" }
  const fetchMock = vi.spyOn(globalThis, "fetch")
  fetchMock
    .mockResolvedValueOnce(new Response(JSON.stringify([user])))
    .mockResolvedValueOnce(new Response(JSON.stringify({ ...user, role: "viewer", active: false })))
    .mockResolvedValueOnce(new Response(JSON.stringify([{ ...user, role: "viewer", active: false }])))

  await renderWithRouter(<UsersPage />)
  await screen.findByText(user.email)
  fireEvent.change(screen.getByLabelText("analyst@example.com のロール"), { target: { value: "viewer" } })
  fireEvent.change(screen.getByLabelText("analyst@example.com の状態"), { target: { value: "inactive" } })
  fireEvent.click(screen.getByRole("button", { name: "analyst@example.com の変更を保存" }))

  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
  const [, init] = fetchMock.mock.calls[1]
  expect(init?.method).toBe("PATCH")
  expect(JSON.parse(String(init?.body))).toEqual({ role: "viewer", active: false })
})

test("resets a password without displaying the submitted secret", async () => {
  const user = { id: "u1", email: "viewer@example.com", role: "viewer", active: true, created_at: "2026-09-11T00:00:00Z", updated_at: "2026-09-11T00:00:00Z" }
  const fetchMock = vi.spyOn(globalThis, "fetch")
  fetchMock
    .mockResolvedValueOnce(new Response(JSON.stringify([user])))
    .mockResolvedValueOnce(new Response(JSON.stringify(user)))
    .mockResolvedValueOnce(new Response(JSON.stringify([user])))

  await renderWithRouter(<UsersPage />)
  await screen.findByText(user.email)
  fireEvent.change(screen.getByLabelText("viewer@example.com の新しいパスワード"), { target: { value: "replacement-password-123" } })
  fireEvent.click(screen.getByRole("button", { name: "viewer@example.com のパスワードをリセット" }))

  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
  const [, init] = fetchMock.mock.calls[1]
  expect(init?.method).toBe("POST")
  expect(JSON.parse(String(init?.body))).toEqual({ password: "replacement-password-123" })
  expect((screen.getByLabelText("viewer@example.com の新しいパスワード") as HTMLInputElement).value).toBe("")
  expect(screen.queryByText("replacement-password-123")).toBeNull()
})

test("withholds every management action when user management is forbidden", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch")
  const descriptor: CapabilityDescriptor = {
    id: "users.manage",
    availability: "forbidden",
    required_permission: "user:manage",
    surfaces: ["ui", "api"],
    reason_code: "permission_required",
    checked_at: "2026-09-12T00:00:00Z",
  }
  const capabilities: Capabilities = {
    auth_mode: "session",
    role: "analyst",
    permissions: [],
    checked_at: descriptor.checked_at,
    data: [descriptor],
  }

  await renderWithI18n(
    <MemoryRouter>
      <SessionContext.Provider value={{
        user: { id: "u1", email: "analyst@example.com", role: "analyst", auth_mode: "session" },
        userState: "identified",
        authMode: "session",
        capabilities,
        loading: false,
        capabilityError: null,
        refresh: () => {},
        logout: async () => {},
        logoutError: null,
        loggingOut: false,
        capabilityFor: (id) => id === descriptor.id ? descriptor : null,
      }}>
        <UsersPage />
      </SessionContext.Provider>
    </MemoryRouter>,
  )

  expect(screen.getByRole("note")).toBeDefined()
  expect(screen.queryByRole("button", { name: "ユーザを作成" })).toBeNull()
  expect(fetchMock).not.toHaveBeenCalled()
})
