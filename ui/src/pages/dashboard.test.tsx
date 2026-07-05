import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { DashboardPage } from "./dashboard"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("shows loading skeleton initially", async () => {
  vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}))
  await renderWithRouter(<DashboardPage />)
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

  await renderWithRouter(<DashboardPage />)

  expect(await screen.findByText("17")).toBeDefined()
  expect(screen.getByText("4")).toBeDefined()
  expect(screen.getAllByText("2").length).toBeGreaterThan(0)
  expect(screen.getByText("42")).toBeDefined()
})

test("shows screening list freshness card when data is present", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        customers_by_risk_tier: {},
        total_customers: 0,
        alerts_by_status: {},
        alerts_by_severity: {},
        total_alerts: 0,
        cases_by_status: {},
        total_cases: 0,
        recent_transactions: 0,
        screening_list_freshness: [
          { list_id: "ofac_sdn", list_type: "sanctions", stale_days: 0, needs_operational_alert: false },
          { list_id: "pep_provider", list_type: "pep", stale_days: 5, needs_operational_alert: true },
        ],
      }),
    ),
  )

  await renderWithRouter(<DashboardPage />)

  expect(await screen.findByText("ofac_sdn")).toBeDefined()
  expect(screen.getByText("pep_provider")).toBeDefined()
  expect(screen.getByText("5日経過")).toBeDefined()
})

test("hides screening list freshness card when no data is present", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        customers_by_risk_tier: {},
        total_customers: 0,
        alerts_by_status: {},
        alerts_by_severity: {},
        total_alerts: 0,
        cases_by_status: {},
        total_cases: 0,
        recent_transactions: 0,
      }),
    ),
  )

  await renderWithRouter(<DashboardPage />)

  await screen.findByText("ダッシュボード")
  expect(screen.queryByText("制裁・PEPリストの鮮度")).toBeNull()
})

test("shows error message on fetch failure", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("network error"))

  await renderWithRouter(<DashboardPage />)

  expect(await screen.findByText(/データの取得に失敗しました/)).toBeDefined()
})
