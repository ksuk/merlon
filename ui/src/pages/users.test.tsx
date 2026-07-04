import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { UsersPage } from "./users"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
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

  renderWithRouter(<UsersPage />)

  expect(await screen.findByText("alice@example.com")).toBeDefined()
  expect(screen.getByText("管理者")).toBeDefined()
})

test("shows empty state", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  renderWithRouter(<UsersPage />)

  expect(await screen.findByText("ユーザが登録されていません")).toBeDefined()
})
