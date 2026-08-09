import { fireEvent, screen, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { ReportsPage } from "./reports"
import { paginatedResponse } from "@/test/api-test-utils"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders STR report form with case-qualified candidate alerts", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.includes("/cases")) return paginatedResponse([{ id: "case-candidate", customer_id: "c1", alert_ids: ["a1"], status: "investigating", priority: "high", str_candidate: true, summary: "candidate case", created_at: "2025-01-10T10:00:00Z", updated_at: "2025-01-10T10:00:00Z" }])
    if (url.includes("/reports/str")) return paginatedResponse([])
    return paginatedResponse([{
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
    }])
  })

  await renderWithRouter(<ReportsPage />)

  const elements = await screen.findAllByText("STRレポート作成")
  expect(elements.length).toBeGreaterThanOrEqual(1)
  expect(screen.getByText("疑わしい大口送金")).toBeDefined()
})

test("shows empty alert state", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([]))

  await renderWithRouter(<ReportsPage />)

  expect(await screen.findByText("対象となるアラートがありません")).toBeDefined()
})

test("lists a persisted draft and reopens it after remount", async () => {
  const report = {
    id: "report-62",
    alert_id: "a1",
    customer_id: "c1",
    report_type: "str",
    status: "draft",
    suspicious_point: "保存された疑わしい取引",
    transaction_ids: ["t1"],
    transaction_snapshot: [],
    total_amount: 100,
    currency: "JPY",
    created_at: "2026-08-01T10:00:00Z",
    updated_at: "2026-08-01T10:00:00Z",
    created_by: "analyst01",
  }
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/reports/str/report-62")) {
      return new Response(JSON.stringify(report), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    }
    if (url.includes("/reports/str")) return paginatedResponse([report])
    return paginatedResponse([])
  })

  const view = await renderWithRouter(<ReportsPage />)
  fireEvent.click(await screen.findByRole("button", { name: "下書きを開く" }))
  expect(await screen.findByText("下書きID: report-62")).toBeDefined()

  view.unmount()
  await renderWithRouter(<ReportsPage />)
  expect(await screen.findByText("保存された疑わしい取引")).toBeDefined()
})

test("creates a durable draft, remounts, retrieves it by ID, and exports both formats", async () => {
  const draft = {
    id: "report-created-62",
    alert_id: "a-create",
    case_id: "case-candidate",
    customer_id: "c1",
    report_type: "str",
    status: "draft",
    suspicious_point: "作成して再取得する下書き",
    transaction_ids: ["t1"],
    transaction_snapshot: [],
    total_amount: 100,
    currency: "JPY",
    created_at: "2026-08-01T10:00:00Z",
    updated_at: "2026-08-01T10:00:00Z",
    created_by: "analyst01",
  }
  let persisted = false
  let createBody: Record<string, unknown> | null = null
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.includes("/reports/str/export")) return new Response("exported")
    if (init?.method === "POST" && url.endsWith("/reports/str")) {
      persisted = true
      createBody = JSON.parse(String(init.body)) as Record<string, unknown>
      return new Response(JSON.stringify(draft), { status: 201, headers: { "Content-Type": "application/json" } })
    }
    if (url.endsWith("/reports/str/report-created-62")) {
      return new Response(JSON.stringify(draft), { headers: { "Content-Type": "application/json" } })
    }
    if (url.includes("/reports/str")) return paginatedResponse(persisted ? [draft] : [])
    if (url.includes("/cases")) return paginatedResponse([{ id: "case-candidate", customer_id: "c1", alert_ids: ["a-create"], status: "investigating", priority: "high", str_candidate: true, summary: "candidate case", created_at: "2025-01-10T10:00:00Z", updated_at: "2025-01-10T10:00:00Z" }])
    return paginatedResponse([{
      id: "a-create",
      customer_id: "c1",
      scenario_id: "s1",
      severity: "high",
      status: "escalated",
      score: 85.0,
      description: "作成対象アラート",
      transaction_ids: ["t1"],
      detected_at: "2025-01-10T10:00:00Z",
      created_at: "2025-01-10T10:00:00Z",
      updated_at: "2025-01-10T10:00:00Z",
    }])
  })
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:report")
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)

  const view = await renderWithRouter(<ReportsPage />)
  fireEvent.click(await screen.findByRole("button", { name: /作成対象アラート/ }))
  fireEvent.change(screen.getByPlaceholderText("疑わしい取引の理由を記述..."), { target: { value: draft.suspicious_point } })
  fireEvent.change(screen.getByPlaceholderText("担当者名"), { target: { value: "analyst01" } })
  const createButton = screen.getAllByText("STRレポート作成").find((element) => element.tagName === "BUTTON")
  if (!createButton) throw new Error("STR report create button not found")
  fireEvent.click(createButton)
  await waitFor(() => expect(persisted).toBe(true))
  expect(createBody).toMatchObject({ alert_id: "a-create", case_id: "case-candidate" })

  view.unmount()
  await renderWithRouter(<ReportsPage />)
  fireEvent.click(await screen.findByRole("button", { name: "下書きを開く" }))
  expect(await screen.findByText("下書きID: report-created-62")).toBeDefined()

  fireEvent.click(screen.getByRole("button", { name: "JSON" }))
  await waitFor(() => {
    const exportCalls = fetchMock.mock.calls.filter(([input]) => String(input).includes("/reports/str/export?report_id=report-created-62"))
    expect(exportCalls.some(([input]) => String(input).endsWith("format=json"))).toBe(true)
  })
  fireEvent.click(screen.getByRole("button", { name: "CSV" }))
  await waitFor(() => {
    const exportCalls = fetchMock.mock.calls.filter(([input]) => String(input).includes("/reports/str/export?report_id=report-created-62"))
    expect(exportCalls.some(([input]) => String(input).endsWith("format=csv"))).toBe(true)
  })
})

test("keeps a selected draft and its input after an export completeness error", async () => {
  const report = {
    id: "report-export-error",
    alert_id: "a1",
    customer_id: "c1",
    report_type: "str",
    status: "draft",
    suspicious_point: "保持される入力",
    transaction_ids: ["t1"],
    transaction_snapshot: [],
    total_amount: 100,
    currency: "JPY",
    created_at: "2026-08-01T10:00:00Z",
    updated_at: "2026-08-01T10:00:00Z",
    created_by: "analyst01",
  }
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.includes("/reports/str/export")) return new Response(JSON.stringify({ error: "snapshot incomplete", error_code: "validation_failed" }), { status: 422 })
    if (url.endsWith("/reports/str/report-export-error")) return new Response(JSON.stringify(report), { headers: { "Content-Type": "application/json" } })
    if (url.includes("/reports/str")) return paginatedResponse([report])
    return paginatedResponse([])
  })
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:report-error")
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)

  await renderWithRouter(<ReportsPage />)
  fireEvent.click(await screen.findByRole("button", { name: "下書きを開く" }))
  expect(await screen.findByText("下書きID: report-export-error")).toBeDefined()
  fireEvent.click(screen.getByRole("button", { name: "JSON" }))

  expect(await screen.findByRole("alert")).toHaveTextContent("入力内容が正しくありません")
  expect(screen.getByDisplayValue("保持される入力")).toBeDefined()
  expect(screen.getByText("下書きID: report-export-error")).toBeDefined()
})

test("does not offer a high-severity alert without an explicit STR candidate case", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.includes("/cases")) return paginatedResponse([])
    if (url.includes("/reports/str")) return paginatedResponse([])
    return paginatedResponse([{
      id: "high-without-case",
      customer_id: "c1",
      scenario_id: "s1",
      severity: "high",
      status: "escalated",
      score: 95,
      description: "ケース未作成の高重大度アラート",
      transaction_ids: [],
      detected_at: "2025-01-10T10:00:00Z",
      created_at: "2025-01-10T10:00:00Z",
      updated_at: "2025-01-10T10:00:00Z",
    }])
  })

  await renderWithRouter(<ReportsPage />)
  expect(await screen.findByText("対象となるアラートがありません")).toBeDefined()
  expect(screen.queryByText("ケース未作成の高重大度アラート")).toBeNull()
})
