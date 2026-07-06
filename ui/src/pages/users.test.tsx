import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { UsersPage } from "./users"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
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
  expect(screen.getByText("管理者")).toBeDefined()
})

test("shows empty state", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  await renderWithRouter(<UsersPage />)

  expect(await screen.findByText("ユーザが登録されていません")).toBeDefined()
})
