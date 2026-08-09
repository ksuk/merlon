import { screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, expect, test, vi } from "vitest"
import { renderWithI18n } from "@/test/i18n-test-utils"
import type { DashboardStats } from "@/lib/api"
import { DashboardPage } from "./dashboard"

beforeEach(() => {
  vi.restoreAllMocks()
})

const baseStats: DashboardStats = {
  customers_by_risk_tier: { low: 1 },
  total_customers: 1,
  alerts_by_status: { open: 3 },
  alerts_by_severity: { high: 3 },
  total_alerts: 3,
  cases_by_status: { open: 1 },
  total_cases: 1,
  recent_transactions: 0,
  recent_transactions_window_hours: 24,
  exceptions: [],
}

const emptyCounts = {
  open: 0,
  mine: 0,
  unassigned: 0,
  age_buckets: [
    { label: "under_24h", from_hours: 0, to_hours: 24, count: 0 },
    { label: "1_to_3d", from_hours: 24, to_hours: 72, count: 0 },
    { label: "3_to_7d", from_hours: 72, to_hours: 168, count: 0 },
    { label: "over_7d", from_hours: 168, count: 0 },
  ],
}

function statsWith(overrides: Partial<DashboardStats>): DashboardStats {
  return { ...baseStats, ...overrides }
}

function mockDashboard(stats: DashboardStats) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/dashboard")) {
      return Promise.resolve(new Response(JSON.stringify(stats)))
    }
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

function renderDashboard() {
  return renderWithI18n(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

test("an unconfigured SLA policy is stated, not reported as zero overdue", async () => {
  mockDashboard(
    statsWith({
      workload: {
        scope: "analyst@example.com",
        alerts: { ...emptyCounts, open: 3, unassigned: 2 },
        cases: { ...emptyCounts },
        sla: { state: "not_configured", policy_version: "2026-08-08-unset" },
        evaluated_at: "2026-08-08T00:00:00Z",
      },
    }),
  )

  await renderDashboard()

  expect(await screen.findAllByTestId("sla-not-configured")).toHaveLength(2)
  expect(screen.getAllByText(/「超過ゼロ」とは異なります/).length).toBeGreaterThan(0)
  // No overdue tile at all: a zero here would be the untruth this exists to remove.
  expect(screen.queryByText("期限超過")).toBeNull()
})

test("a configured policy shows overdue work and links to it", async () => {
  mockDashboard(
    statsWith({
      workload: {
        scope: "analyst@example.com",
        alerts: { ...emptyCounts, open: 5, mine: 2, unassigned: 1, overdue: 4, due_soon: 1 },
        cases: { ...emptyCounts },
        sla: { state: "running", policy_version: "2026-08-08", due_soon_within_hours: 24 },
        evaluated_at: "2026-08-08T00:00:00Z",
      },
    }),
  )

  await renderDashboard()

  const overdue = await screen.findAllByText("期限超過")
  expect(overdue.length).toBe(2)
  expect(overdue[0].closest("a")?.getAttribute("href")).toBe("/alerts?active=true&overdue=true")
})

test("every workload figure opens the queue that contains exactly those records", async () => {
  mockDashboard(
    statsWith({
      workload: {
        scope: "analyst@example.com",
        alerts: { ...emptyCounts, open: 5, mine: 2, unassigned: 1 },
        cases: { ...emptyCounts },
        sla: { state: "not_configured", policy_version: "unset" },
        evaluated_at: "2026-08-08T00:00:00Z",
      },
    }),
  )

  await renderDashboard()

  const mine = (await screen.findAllByText("自分の担当"))[0]
  expect(mine.closest("a")?.getAttribute("href")).toBe("/alerts?active=true&mine=true")

  const unassigned = screen.getAllByText("未割当")[0]
  expect(unassigned.closest("a")?.getAttribute("href")).toBe("/alerts?active=true&unassigned=true")
})

test("without a signed-in operator there is no 'mine' count to show", async () => {
  mockDashboard(
    statsWith({
      workload: {
        scope: "",
        alerts: { ...emptyCounts, open: 5 },
        cases: { ...emptyCounts },
        sla: { state: "not_configured", policy_version: "unset" },
        evaluated_at: "2026-08-08T00:00:00Z",
      },
    }),
  )

  await renderDashboard()

  // Zero would say nobody is assigned any work, which is a different claim
  // from not knowing who is asking.
  expect(await screen.findAllByText("サインイン中の担当者がいないため集計できません")).toHaveLength(2)
})

test("operational exceptions name the queue that explains them", async () => {
  mockDashboard(
    statsWith({
      exceptions: [
        { kind: "pending_evaluations_failed", count: 7, href: "/pending-evaluations?status=failed", state: "failed" },
      ],
    }),
  )

  await renderDashboard()

  expect(await screen.findByText("再試行待ちの失敗した評価")).toBeDefined()
  expect(screen.getByText("キューを開く").getAttribute("href")).toBe("/pending-evaluations?status=failed")
})

test("an empty exception list is distinguished from nothing having been checked", async () => {
  mockDashboard(statsWith({ exceptions: [] }))

  await renderDashboard()

  expect(await screen.findByText("失敗・劣化している運用作業はありません。")).toBeDefined()
})
