import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AlertsPage } from "./alerts"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders alert table with data", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          id: "a1",
          customer_id: "c1",
          scenario_id: "large_tx",
          severity: "high",
          status: "open",
          score: 88.5,
          description: "大口取引の検出",
          transaction_ids: ["t1"],
          detected_at: "2025-01-15T10:00:00Z",
          created_at: "2025-01-15T10:00:00Z",
          updated_at: "2025-01-15T10:00:00Z",
        },
      ]),
    ),
  )

  renderWithRouter(<AlertsPage />)

  expect(await screen.findByText("高")).toBeDefined()
  expect(screen.getByText("未対応")).toBeDefined()
  expect(screen.getByText("大口取引の検出")).toBeDefined()
})

test("shows empty state when no alerts", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  renderWithRouter(<AlertsPage />)

  expect(await screen.findByText("アラートがありません")).toBeDefined()
})
