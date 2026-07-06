import { fireEvent, screen, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { AuditPage } from "./audit"
import { api } from "@/lib/api"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

function paginatedResponse(data: unknown[]) {
  return new Response(JSON.stringify({ data, pagination: { has_more: false } }))
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders audit log entries", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    paginatedResponse([
      {
        id: 1,
        user_id: "admin",
        action: "create",
        resource_type: "customers",
        resource_id: "c1",
        ip_address: "192.168.1.1",
        created_at: "2025-01-15T10:00:00Z",
      },
      {
        id: 2,
        user_id: "operator",
        action: "score_customer",
        resource_type: "customers",
        resource_id: "c1",
        ip_address: "192.168.1.2",
        created_at: "2025-01-15T11:00:00Z",
      },
    ]),
  )

  await renderWithRouter(<AuditPage />)

  expect(await screen.findByText("監査ログ")).toBeDefined()
  expect(screen.getByText("作成")).toBeDefined()
  expect(screen.getByText("スコアリング")).toBeDefined()
  expect(screen.getByText("admin")).toBeDefined()
})

test("shows empty state when no entries", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(paginatedResponse([]))

  await renderWithRouter(<AuditPage />)

  expect(await screen.findByText("監査ログがありません")).toBeDefined()
})

test("filters by date range", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(paginatedResponse([]))

  renderWithRouter(<AuditPage />)
  await screen.findByText("監査ログがありません")
  fetchSpy.mockClear()

  fireEvent.change(screen.getByLabelText("期間（開始）"), {
    target: { value: "2026-01-01T00:00" },
  })

  await waitFor(() => {
    expect(fetchSpy).toHaveBeenCalled()
  })
  const url = new URL(fetchSpy.mock.calls[0][0] as string, "http://localhost")
  expect(url.searchParams.get("since")).toBe(new Date("2026-01-01T00:00").toISOString())
})

test("filters by resource", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(paginatedResponse([]))

  renderWithRouter(<AuditPage />)
  await screen.findByText("監査ログがありません")
  fetchSpy.mockClear()

  fireEvent.change(screen.getByLabelText("対象リソースID"), {
    target: { value: "cust-123" },
  })

  await waitFor(() => {
    expect(fetchSpy).toHaveBeenCalled()
  })
  const url = new URL(fetchSpy.mock.calls[0][0] as string, "http://localhost")
  expect(url.searchParams.get("resource_id")).toBe("cust-123")
})

test("export button triggers download", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(paginatedResponse([]))
  const exportSpy = vi.spyOn(api.audit, "export").mockResolvedValue(undefined)

  renderWithRouter(<AuditPage />)
  await screen.findByText("監査ログがありません")

  fireEvent.click(screen.getByRole("button", { name: /CSV/ }))

  await waitFor(() => {
    expect(exportSpy).toHaveBeenCalledTimes(1)
  })
})

test("renders diff view for rule changes", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    paginatedResponse([
      {
        id: 3,
        user_id: "admin",
        action: "update",
        resource_type: "rules",
        resource_id: "cdd_basic",
        details: { diff: JSON.stringify({ threshold: { before: 1, after: 2 } }) },
        created_at: "2025-01-15T12:00:00Z",
      },
    ]),
  )

  renderWithRouter(<AuditPage />)
  await screen.findByText("cdd_basic")

  fireEvent.click(screen.getByText("cdd_basic"))

  expect(await screen.findByText("threshold")).toBeDefined()
  expect(screen.getByText("1")).toBeDefined()
  expect(screen.getByText("2")).toBeDefined()
})
