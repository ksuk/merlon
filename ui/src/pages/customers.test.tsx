import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { CustomersPage } from "./customers"
import { paginatedResponse } from "@/test/api-test-utils"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders customer table with data", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    paginatedResponse([
        {
          id: "c1",
          external_id: "EXT-001",
          customer_type: "individual",
          country_code: "JP",
          product_types: ["crypto"],
          attributes: {},
          risk_score: 45.2,
          risk_tier: "medium",
          last_scored_at: "2025-01-15T00:00:00Z",
          created_at: "2025-01-01T00:00:00Z",
          updated_at: "2025-01-15T00:00:00Z",
        },
      ]),
  )

  await renderWithRouter(<CustomersPage />)

  expect(await screen.findByText("EXT-001")).toBeDefined()
  expect(screen.getByText("個人")).toBeDefined()
  expect(screen.getByText("JP")).toBeDefined()
  expect(screen.getByText("45.2")).toBeDefined()
  expect(screen.getByText("中")).toBeDefined()
})

test("shows empty state when no customers", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(paginatedResponse([]))

  await renderWithRouter(<CustomersPage />)

  expect(await screen.findByText("顧客データがありません")).toBeDefined()
})
