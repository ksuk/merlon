import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { DashboardPage } from "./dashboard"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("shows loading skeleton initially", () => {
  vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}))
  renderWithRouter(<DashboardPage />)
  const skeletons = document.querySelectorAll(".animate-pulse")
  expect(skeletons.length).toBeGreaterThan(0)
})

test("renders stat cards after data loads", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        customers_by_risk_tier: { low: 10, medium: 5, high: 2 },
        total_customers: 17,
        alerts_by_status: { open: 3, investigating: 1 },
        alerts_by_severity: { high: 2, critical: 1 },
        total_alerts: 4,
        cases_by_status: { open: 2 },
        total_cases: 2,
        recent_transactions: 42,
      }),
    ),
  )

  renderWithRouter(<DashboardPage />)

  expect(await screen.findByText("17")).toBeDefined()
  expect(screen.getByText("4")).toBeDefined()
  expect(screen.getAllByText("2").length).toBeGreaterThan(0)
  expect(screen.getByText("42")).toBeDefined()
})

test("shows error message on fetch failure", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("network error"))

  renderWithRouter(<DashboardPage />)

  expect(await screen.findByText(/データの取得に失敗しました/)).toBeDefined()
})
