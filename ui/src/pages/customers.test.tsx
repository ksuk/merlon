import { fireEvent, screen } from "@testing-library/react"
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
  // A fresh Response per call: the page reads the customer list and the
  // kyc_required_fields policy, and a single Response body can only be read
  // once.
  vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
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
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([]))

  await renderWithRouter(<CustomersPage />)

  expect(await screen.findByText("顧客データがありません")).toBeDefined()
})

test("loads a customer from the second cursor page", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).includes("cursor=page-2")) {
      return new Response(JSON.stringify({
        data: [{
          id: "c2",
          external_id: "EXT-SECOND-PAGE",
          customer_type: "individual",
          country_code: "JP",
          product_types: [],
          attributes: {},
          created_at: "2025-01-01T00:00:00Z",
          updated_at: "2025-01-01T00:00:00Z",
        }],
        pagination: { has_more: false },
      }), { status: 200 })
    }
    return new Response(JSON.stringify({
      data: [{
        id: "c1",
        external_id: "EXT-FIRST-PAGE",
        customer_type: "individual",
        country_code: "JP",
        product_types: [],
        attributes: {},
        created_at: "2025-01-02T00:00:00Z",
        updated_at: "2025-01-02T00:00:00Z",
      }],
      pagination: { has_more: true, next_cursor: "page-2" },
    }), { status: 200 })
  })

  await renderWithRouter(<CustomersPage />)

  expect(await screen.findByText("EXT-SECOND-PAGE")).toBeDefined()
  expect(fetchMock.mock.calls.some(([input]) => String(input).includes("cursor=page-2"))).toBe(true)
})

test("sends customer search to the server", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    const externalId = url.includes("search=NEEDLE") ? "EXT-SERVER-MATCH" : "EXT-FIRST-PAGE"
    return paginatedResponse([{
      id: externalId,
      external_id: externalId,
      customer_type: "individual",
      country_code: "JP",
      product_types: [],
      attributes: {},
      created_at: "2025-01-01T00:00:00Z",
      updated_at: "2025-01-01T00:00:00Z",
    }])
  })

  await renderWithRouter(<CustomersPage />)
  await screen.findByText("EXT-FIRST-PAGE")
  fireEvent.change(screen.getByPlaceholderText("ID・名前・国コードで検索..."), {
    target: { value: "NEEDLE" },
  })

  expect(await screen.findByText("EXT-SERVER-MATCH")).toBeDefined()
  expect(fetchMock.mock.calls.some(([input]) => String(input).includes("search=NEEDLE"))).toBe(true)
})
