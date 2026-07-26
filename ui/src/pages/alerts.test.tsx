import { screen, fireEvent, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { AlertsPage } from "./alerts"

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
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([sampleAlert])))

  await renderWithRouter(<AlertsPage />)

  await screen.findByText("大口取引の検出")
  // "高" also appears as a <select> option in the bulk-close filter form, so
  // scope this assertion to the severity badge specifically.
  expect(screen.getByLabelText("select-a1").closest("tr")?.textContent).toContain("高")
  expect(screen.getByText("未対応")).toBeDefined()
})

test("shows empty state when no alerts", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  await renderWithRouter(<AlertsPage />)

  expect(await screen.findByText("アラートがありません")).toBeDefined()
})

test("bulk close requires a reason before submitting", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([sampleAlert])))

  await renderWithRouter(<AlertsPage />)
  await screen.findByText("大口取引の検出")

  const closeButton = screen.getByText("条件に一致するアラートをクローズ")
  expect(closeButton).toHaveProperty("disabled", true)
})

test("bulk close calls the filter-based bulk-close endpoint", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch")
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify([sampleAlert])))

  await renderWithRouter(<AlertsPage />)
  await screen.findByText("大口取引の検出")

  fireEvent.change(screen.getByPlaceholderText("判断理由（必須）"), {
    target: { value: "既知の許容パターン" },
  })

  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ closed_count: 1, alert_ids: ["a1"] })))
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify([])))

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
  const fetchMock = vi.spyOn(globalThis, "fetch")
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify([sampleAlert])))

  await renderWithRouter(<AlertsPage />)
  await screen.findByText("大口取引の検出")

  expect(screen.queryByText("選択したアラートをケースにまとめる")).toBeNull()

  fireEvent.click(screen.getByLabelText("select-a1"))

  expect(await screen.findByText("選択中: 1 件")).toBeDefined()

  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ case_id: "case-1", created: true })))
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify([])))

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
