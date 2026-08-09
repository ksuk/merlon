import { fireEvent, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { BatchPage } from "./batch"
import { paginatedResponse } from "@/test/api-test-utils"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders batch page with customers", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    paginatedResponse([
        {
          id: "c1",
          external_id: "EXT-001",
          customer_type: "individual",
          country_code: "JP",
          product_types: [],
          attributes: {},
          created_at: "2025-01-01T00:00:00Z",
          updated_at: "2025-01-01T00:00:00Z",
        },
      ]),
  )

  await renderWithRouter(<BatchPage />)

  expect(await screen.findByText("一括処理")).toBeDefined()
  expect(screen.getByText("EXT-001")).toBeDefined()
  expect(screen.getByText("一括スコアリング")).toBeDefined()
  expect(screen.getByText("一括モニタリング")).toBeDefined()
})

test("shows error state", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  await renderWithRouter(<BatchPage />)

  expect(await screen.findByText("データの取得に失敗しました")).toBeDefined()
})

test("marks queued PENDING_REVIEW results as requiring operator review", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.includes("/customers?")) {
      return paginatedResponse([{
        id: "c1",
        external_id: "EXT-001",
        customer_type: "individual",
        country_code: "JP",
        product_types: [],
        attributes: {},
        created_at: "2025-01-01T00:00:00Z",
        updated_at: "2025-01-01T00:00:00Z",
      }])
    }
    if (url.includes("/batch/runs?")) {
      return paginatedResponse([])
    }
    if (url.endsWith("/batch/targets/preview")) {
      return new Response(JSON.stringify({ id: "manifest-1", operation: "batch_monitor", target_mode: "selected", customer_ids: ["c1"], sample_customer_ids: ["c1"], target_count: 1, criteria: "selected customers", token: "token-1", status: "preview", version: 1, expires_at: "2099-01-01T00:00:00Z", created_at: "2026-01-01T00:00:00Z" }), { status: 200, headers: { "Content-Type": "application/json" } })
    }
    if (url.endsWith("/batch/targets/manifest-1/confirm")) {
      return new Response(JSON.stringify({ id: "manifest-1", operation: "batch_monitor", target_mode: "selected", customer_ids: ["c1"], sample_customer_ids: ["c1"], target_count: 1, criteria: "selected customers", status: "confirmed", version: 2, expires_at: "2099-01-01T00:00:00Z", created_at: "2026-01-01T00:00:00Z" }), { status: 200, headers: { "Content-Type": "application/json" } })
    }
    if (url.endsWith("/batch/runs")) {
      return new Response(JSON.stringify({ id: "run-1", job_type: "batch_monitor", operation: "batch_monitor", status: "partial", parameters: {}, target_manifest_id: "manifest-1", result_counts: { queued_for_review: 1 }, started_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }), { status: 201, headers: { "Content-Type": "application/json" } })
    }
    throw new Error(`unexpected request: ${url}`)
  })

  await renderWithRouter(<BatchPage />)
  await screen.findByText("EXT-001")
  fireEvent.click(screen.getByText("EXT-001"))
  fireEvent.change(screen.getByLabelText("操作理由"), { target: { value: "recover monitor queue" } })
  fireEvent.click(screen.getByRole("button", { name: "一括モニタリング" }))
  await screen.findByText("対象マニフェストの確認")
  fireEvent.click(screen.getByRole("button", { name: "確認して永続実行を開始" }))

  expect(await screen.findByRole("alert")).toBeDefined()
  expect(screen.getByText("要確認")).toBeDefined()
  expect(screen.getAllByText("PENDING_REVIEW").length).toBeGreaterThan(0)
})
