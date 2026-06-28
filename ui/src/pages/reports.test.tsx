import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { ReportsPage } from "./reports"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders STR report form with alerts", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          id: "a1",
          customer_id: "c1",
          scenario_id: "s1",
          severity: "high",
          status: "escalated",
          score: 85.0,
          description: "疑わしい大口送金",
          transaction_ids: ["t1"],
          detected_at: "2025-01-10T10:00:00Z",
          created_at: "2025-01-10T10:00:00Z",
          updated_at: "2025-01-10T10:00:00Z",
        },
      ]),
    ),
  )

  renderWithRouter(<ReportsPage />)

  const elements = await screen.findAllByText("STRレポート作成")
  expect(elements.length).toBeGreaterThanOrEqual(1)
  expect(screen.getByText("疑わしい大口送金")).toBeDefined()
})

test("shows empty alert state", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  renderWithRouter(<ReportsPage />)

  expect(await screen.findByText("対象となるアラートがありません")).toBeDefined()
})
