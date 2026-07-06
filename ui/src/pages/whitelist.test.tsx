import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { WhitelistPage } from "./whitelist"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

const adminUser = { id: "u-admin", email: "admin@example.com", role: "admin" }

const pendingEntry = {
  id: "wl-pending",
  customer_id: "cust-1",
  status: "pending_approval",
  reason: "trusted long-standing customer",
  valid_from: "2026-07-01T00:00:00Z",
  valid_until: "2026-10-01T00:00:00Z",
  requested_by: "u-analyst",
  version: 1,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
}

const activeEntry = {
  id: "wl-active",
  customer_id: "cust-2",
  status: "active",
  reason: "approved long ago",
  valid_from: "2026-01-01T00:00:00Z",
  valid_until: "2026-12-01T00:00:00Z",
  requested_by: "u-analyst",
  approved_by: "u-admin",
  approved_at: "2026-01-02T00:00:00Z",
  version: 2,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
}

function mockFetchRouting(user: unknown, entries: unknown[]) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/auth/me")) {
      return Promise.resolve(new Response(JSON.stringify(user)))
    }
    if (url.includes("/whitelist")) {
      return Promise.resolve(
        new Response(JSON.stringify({ data: entries, pagination: { has_more: false } })),
      )
    }
    return Promise.resolve(new Response(JSON.stringify({})))
  })
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders pending and active entries", async () => {
  mockFetchRouting(adminUser, [pendingEntry, activeEntry])

  await renderWithRouter(<WhitelistPage />)

  expect(await screen.findByText("承認待ち")).toBeDefined()
  expect(screen.getByText("有効")).toBeDefined()
  expect(screen.getByText("trusted long-standing customer")).toBeDefined()
  expect(screen.getByText("approved long ago")).toBeDefined()
})

test("shows approve button disabled for requester", async () => {
  const ownRequestEntry = { ...pendingEntry, requested_by: adminUser.id }
  mockFetchRouting(adminUser, [ownRequestEntry])

  await renderWithRouter(<WhitelistPage />)

  const approveButton = await screen.findByRole("button", { name: "承認" })
  // Server enforces this too (403 on self-approval, whitelist.md §1); the
  // disabled state here is only a UX hint.
  expect(approveButton.hasAttribute("disabled")).toBe(true)
})

test("shows empty state", async () => {
  mockFetchRouting(adminUser, [])

  await renderWithRouter(<WhitelistPage />)

  expect(await screen.findByText("ホワイトリストエントリがありません")).toBeDefined()
})
