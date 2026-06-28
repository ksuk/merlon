import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { BatchPage } from "./batch"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders batch page with customers", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
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
    ),
  )

  renderWithRouter(<BatchPage />)

  expect(await screen.findByText("一括処理")).toBeDefined()
  expect(screen.getByText("EXT-001")).toBeDefined()
  expect(screen.getByText("一括スコアリング")).toBeDefined()
  expect(screen.getByText("一括モニタリング")).toBeDefined()
})

test("shows error state", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  renderWithRouter(<BatchPage />)

  expect(await screen.findByText("データの取得に失敗しました")).toBeDefined()
})
