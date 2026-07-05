import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { CasesPage } from "./cases"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

function makeCase(id: string, status: string) {
  return {
    id,
    customer_id: "c1",
    alert_ids: [],
    status,
    priority: "medium",
    summary: `${status} case`,
    created_at: "2025-01-15T10:00:00Z",
    updated_at: "2025-01-15T10:00:00Z",
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("displays new/reopened/str_filed status badges", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        makeCase("case-new", "new"),
        makeCase("case-reopened", "reopened"),
        makeCase("case-str", "str_filed"),
      ]),
    ),
  )

  renderWithRouter(<CasesPage />)

  expect(await screen.findByText("再オープン")).toBeDefined()
  expect(screen.getByText("STR対象")).toBeDefined()
  expect(screen.getAllByText("新規").length).toBeGreaterThan(0)
})
