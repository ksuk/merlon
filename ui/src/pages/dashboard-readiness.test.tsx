import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { DashboardPage } from "./dashboard"

// #70 asks for the aggregate stop condition and each readiness state to be
// distinguishable. The dashboard's freshness tests predate the readiness card,
// so nothing covered the one assertion that matters: an unimported or
// unreadable source must not read as healthy.

function statsWith(overrides: Record<string, unknown>) {
  return {
    total_customers: 10,
    customers_by_risk_tier: {},
    alerts_by_status: {},
    alerts_by_severity: {},
    cases_by_status: {},
    recent_transactions: 0,
    unresolved_alerts: 0,
    open_cases: 0,
    ...overrides,
  }
}

function mockStats(stats: Record<string, unknown>) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    if (url.includes("/dashboard")) return Promise.resolve(new Response(JSON.stringify(stats)))
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

async function renderDashboard() {
  await renderWithI18n(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("a ready screening surface reports ready", async () => {
  mockStats(statsWith({ screening_ready: true, screening_degraded_sources: [] }))
  await renderDashboard()

  expect(await screen.findByText("正常")).toBeDefined()
})

test("a degraded screening surface names the sources rather than reporting healthy", async () => {
  mockStats(statsWith({ screening_ready: false, screening_degraded_sources: ["ofac_sdn", "un_consolidated"] }))
  await renderDashboard()

  expect(await screen.findByText("不完全")).toBeDefined()
  // Naming which source is degraded is what makes the stop condition
  // actionable; "not ready" alone leaves the operator to guess.
  expect(screen.getByText(/ofac_sdn/)).toBeDefined()
  expect(screen.getByText(/un_consolidated/)).toBeDefined()
})

test.each([
  ["never_imported", "未取込"],
  ["unreadable", "読取不可"],
  ["failed", "取込失敗"],
  ["stale", "古いデータ"],
])("a %s source is labelled rather than shown as fresh", async (state, label) => {
  mockStats(
    statsWith({
      screening_ready: false,
      screening_degraded_sources: ["list_a"],
      screening_list_freshness: [
        { list_id: "list_a", operational_state: state, age_seconds: 0, threshold_seconds: 259200 },
      ],
    }),
  )
  await renderDashboard()

  expect(await screen.findByText(label)).toBeDefined()
})
