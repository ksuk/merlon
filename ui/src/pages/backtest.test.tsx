import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { BacktestPage } from "./backtest"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders backtest form with customers", async () => {
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

  renderWithRouter(<BacktestPage />)

  expect(await screen.findByText("EXT-001")).toBeDefined()
  expect(screen.getByText("バックテスト実行")).toBeDefined()
})

test("shows error state", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  renderWithRouter(<BacktestPage />)

  expect(await screen.findByText("データの取得に失敗しました")).toBeDefined()
})
