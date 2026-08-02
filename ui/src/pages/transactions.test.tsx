import { fireEvent, screen } from "@testing-library/react"
import { beforeEach, expect, test, vi } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { paginatedResponse } from "@/test/api-test-utils"
import { TransactionsPage } from "./transactions"

const customer = {
  id: "c1",
  external_id: "EXT-001",
  customer_type: "individual",
  country_code: "JP",
  product_types: [],
  attributes: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
}

function renderPage() {
  return renderWithI18n(
    <MemoryRouter>
      <TransactionsPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("does not request transactions until an explicit customer is selected", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/customers")) return paginatedResponse([customer])
    if (url.includes("/transactions?customer_id=c1")) {
      return paginatedResponse([{
        id: "tx1",
        customer_id: "c1",
        external_id: "TX-001",
        amount: 1000,
        currency: "JPY",
        direction: "inbound",
        executed_at: "2026-01-02T00:00:00Z",
        created_at: "2026-01-02T00:00:00Z",
      }])
    }
    throw new Error(`unexpected request: ${url}`)
  })

  await renderPage()
  await screen.findByRole("option", { name: /EXT-001/ })

  expect(fetchMock.mock.calls.some(([input]) => String(input).includes("/transactions"))).toBe(false)

  fireEvent.change(screen.getByRole("combobox", { name: "顧客スコープ" }), { target: { value: "c1" } })

  expect(await screen.findByRole("link", { name: "入金" })).toBeDefined()
  const transactionRequest = fetchMock.mock.calls.find(([input]) => String(input).includes("/transactions"))
  expect(String(transactionRequest?.[0])).toContain("/transactions?customer_id=c1")
})

test("keeps registration available when the scoped list fails", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/customers")) return paginatedResponse([customer])
    if (url.includes("/transactions?customer_id=c1")) {
      return new Response(JSON.stringify({ error: "temporary failure" }), { status: 500 })
    }
    throw new Error(`unexpected request: ${url}`)
  })

  await renderPage()
  await screen.findByRole("option", { name: /EXT-001/ })
  fireEvent.change(screen.getByRole("combobox", { name: "顧客スコープ" }), { target: { value: "c1" } })

  expect(await screen.findByRole("alert")).toBeDefined()
  fireEvent.click(screen.getByRole("button", { name: "新規登録" }))
  expect(await screen.findByText("取引登録")).toBeDefined()
  expect(screen.getByRole("button", { name: "登録" })).toBeDefined()
})

test("shows an empty state for a selected customer with no transactions", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/customers")) return paginatedResponse([customer])
    if (url.includes("/transactions?customer_id=c1")) return paginatedResponse([])
    throw new Error(`unexpected request: ${url}`)
  })

  await renderPage()
  await screen.findByRole("option", { name: /EXT-001/ })
  fireEvent.change(screen.getByRole("combobox", { name: "顧客スコープ" }), { target: { value: "c1" } })

  expect(await screen.findByText("取引データがありません")).toBeDefined()
})
