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
    if (url.endsWith("/customers")) {
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
    if (url.endsWith("/batch/monitor")) {
      return new Response(JSON.stringify({
        total: 1,
        succeeded: 0,
        failed: 0,
        queued_for_review: 1,
        alerts_total: 0,
        results: [{ customer_id: "c1", alerts_raised: 0, pending_review: true }],
        duration: "10ms",
      }), { status: 200, headers: { "Content-Type": "application/json" } })
    }
    throw new Error(`unexpected request: ${url}`)
  })

  await renderWithRouter(<BatchPage />)
  await screen.findByText("EXT-001")
  fireEvent.click(screen.getByRole("button", { name: "一括モニタリング" }))

  expect(await screen.findByRole("alert")).toBeDefined()
  expect(screen.getByText("要確認")).toBeDefined()
  expect(screen.getAllByText("PENDING_REVIEW").length).toBeGreaterThan(0)
})
