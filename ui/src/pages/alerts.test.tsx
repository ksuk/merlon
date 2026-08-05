import { screen, fireEvent, waitFor, within } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { AlertsPage } from "./alerts"
import { paginatedResponse } from "@/test/api-test-utils"

const sampleAlert = {
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
}

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders alert table with data", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([sampleAlert]))

  await renderWithRouter(<AlertsPage />)

  await screen.findByText("大口取引の検出")
  // "高" also appears as a <select> option in the bulk-close filter form, so
  // scope this assertion to the severity badge specifically.
  expect(screen.getByLabelText("select-a1").closest("tr")?.textContent).toContain("高")
  const row = screen.getByLabelText("select-a1").closest("tr")
  expect(row).not.toBeNull()
  expect(within(row!).getByText("未対応")).toBeDefined()
})

test("shows empty state when no alerts", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([]))

  await renderWithRouter(<AlertsPage />)

  expect(await screen.findByText("アラートがありません")).toBeDefined()
})

test("loads an alert from the second cursor page", async () => {
  const secondAlert = { ...sampleAlert, id: "a2", description: "2ページ目のアラート" }
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
    return new Response(JSON.stringify({
      data: [sampleAlert],
      pagination: { has_more: true, next_cursor: "page-2" },
    }), { status: 200, headers: { "Content-Type": "application/json" } })
  })

  await renderWithRouter(<AlertsPage />)

  expect(await screen.findByText("大口取引の検出")).toBeDefined()
  const next = screen.getByRole("button", { name: "次へ" })
  fetchMock.mockImplementation(async (input) => {
    if (String(input).includes("cursor=page-2")) return paginatedResponse([secondAlert])
    return paginatedResponse([sampleAlert])
  })
  fireEvent.click(next)
  expect(await screen.findByText("2ページ目のアラート")).toBeDefined()
  expect(fetchMock.mock.calls.some(([input]) => String(input).includes("cursor=page-2"))).toBe(true)
})

test("renders critical alerts before lower-risk alerts", async () => {
  const low = { ...sampleAlert, id: "low", severity: "low", description: "低リスク" }
  const critical = { ...sampleAlert, id: "critical", severity: "critical", description: "重大リスク" }
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([low, critical]))

  await renderWithRouter(<AlertsPage />)

  await screen.findByText("重大リスク")
  const rows = screen.getAllByRole("row").slice(1)
  expect(rows[0]).toHaveTextContent("重大リスク")
  expect(rows[1]).toHaveTextContent("低リスク")
})

test("bulk close requires a reason before submitting", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([sampleAlert]))

  await renderWithRouter(<AlertsPage />)
  await screen.findByText("大口取引の検出")

  const closeButton = screen.getByText("条件に一致するアラートをクローズ")
  expect(closeButton).toHaveProperty("disabled", true)
})

test("bulk close calls the filter-based bulk-close endpoint", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).includes("/alerts/bulk-close")) return new Response(JSON.stringify({ closed_count: 1, alert_ids: ["a1"] }))
    return paginatedResponse([sampleAlert])
  })

  await renderWithRouter(<AlertsPage />)
  await screen.findByText("大口取引の検出")

  fireEvent.change(screen.getByPlaceholderText("判断理由（必須）"), {
    target: { value: "既知の許容パターン" },
  })

  fireEvent.click(screen.getByText("条件に一致するアラートをクローズ"))

  await waitFor(() => {
    const bulkCloseCall = fetchMock.mock.calls.find(([url]) =>
      String(url).includes("/alerts/bulk-close"),
    )
    expect(bulkCloseCall).toBeDefined()
  })

  const [, init] = fetchMock.mock.calls.find(([url]) => String(url).includes("/alerts/bulk-close"))!
  const body = JSON.parse((init as RequestInit).body as string)
  expect(body.reason).toBe("既知の許容パターン")
})

test("selecting an alert enables bulk case assignment", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).includes("/alerts/bulk-case")) return new Response(JSON.stringify({ case_id: "case-1", created: true }))
    return paginatedResponse([sampleAlert])
  })

  await renderWithRouter(<AlertsPage />)
  await screen.findByText("大口取引の検出")

  expect(screen.queryByText("選択したアラートをケースにまとめる")).toBeNull()

  fireEvent.click(screen.getByLabelText("select-a1"))

  expect(await screen.findByText("選択中: 1 件")).toBeDefined()

  fireEvent.click(screen.getByText("選択したアラートをケースにまとめる"))

  await waitFor(() => {
    const bulkCaseCall = fetchMock.mock.calls.find(([url]) => String(url).includes("/alerts/bulk-case"))
    expect(bulkCaseCall).toBeDefined()
  })

  const [, init] = fetchMock.mock.calls.find(([url]) => String(url).includes("/alerts/bulk-case"))!
  const body = JSON.parse((init as RequestInit).body as string)
  expect(body.alert_ids).toEqual(["a1"])
  expect(body.customer_id).toBe("c1")
})

test("restores URL queue filters and sends composable my-work/SLA/search filters", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([sampleAlert]))

  await renderWithI18n(
    <MemoryRouter initialEntries={["/alerts?status=investigating&mine=true&overdue=true&search=customer-1"]}>
      <AlertsPage />
    </MemoryRouter>,
  )

  await screen.findByText("大口取引の検出")
  expect(screen.getByLabelText("ステータス")).toHaveValue("investigating")
  expect(screen.getByRole("checkbox", { name: "自分の担当" })).toBeChecked()
  expect(screen.getByRole("checkbox", { name: "期限超過" })).toBeChecked()
  expect(screen.getByLabelText("検索")).toHaveValue("customer-1")
  const queueRequest = fetchMock.mock.calls.find(([input]) => String(input).includes("/alerts?"))
  expect(String(queueRequest?.[0])).toContain("status=investigating")
  expect(String(queueRequest?.[0])).toContain("mine=true")
  expect(String(queueRequest?.[0])).toContain("overdue=true")
  expect(String(queueRequest?.[0])).toContain("search=customer-1")
})
