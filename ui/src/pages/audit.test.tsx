import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AuditPage } from "./audit"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders audit log entries", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          id: 1,
          user_id: "admin",
          action: "create",
          resource_type: "customers",
          resource_id: "c1",
          ip_address: "192.168.1.1",
          created_at: "2025-01-15T10:00:00Z",
        },
        {
          id: 2,
          user_id: "operator",
          action: "score_customer",
          resource_type: "customers",
          resource_id: "c1",
          ip_address: "192.168.1.2",
          created_at: "2025-01-15T11:00:00Z",
        },
      ]),
    ),
  )

  renderWithRouter(<AuditPage />)

  expect(await screen.findByText("監査ログ")).toBeDefined()
  expect(screen.getByText("作成")).toBeDefined()
  expect(screen.getByText("スコアリング")).toBeDefined()
  expect(screen.getByText("admin")).toBeDefined()
})

test("shows empty state when no entries", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  renderWithRouter(<AuditPage />)

  expect(await screen.findByText("監査ログがありません")).toBeDefined()
})
