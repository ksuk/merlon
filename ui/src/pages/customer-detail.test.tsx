import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { CustomerDetailPage } from "./customer-detail"

function renderWithRoute(id: string) {
  return renderWithI18n(
    <MemoryRouter initialEntries={[`/customers/${id}`]}>
      <Routes>
        <Route path="customers/:id" element={<CustomerDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders customer detail with profile data", async () => {
  let callCount = 0
  vi.spyOn(globalThis, "fetch").mockImplementation(() => {
    callCount++
    if (callCount === 1) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            id: "c1",
            external_id: "EXT-001",
            customer_type: "individual",
            country_code: "JP",
            product_types: ["crypto"],
            attributes: { name: "Test" },
            risk_score: 45.2,
            risk_tier: "medium",
            last_scored_at: "2025-01-15T00:00:00Z",
            created_at: "2025-01-01T00:00:00Z",
            updated_at: "2025-01-15T00:00:00Z",
          }),
        ),
      )
    }
    return Promise.resolve(new Response(JSON.stringify([])))
  })

  await renderWithRoute("c1")

  expect(await screen.findByText("EXT-001")).toBeDefined()
  expect(screen.getByText("個人")).toBeDefined()
  expect(screen.getAllByText("中リスク").length).toBeGreaterThan(0)
  expect(screen.getByText("スコアリング")).toBeDefined()
})

test("shows error for missing customer", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("not found"))

  await renderWithRoute("nonexistent")

  expect(await screen.findByText("顧客データの取得に失敗しました")).toBeDefined()
})
