import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { TransactionDetailPage } from "./transaction-detail"

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders transaction detail", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        id: "txn-001",
        customer_id: "cust-001",
        external_id: "WIRE-001",
        amount: 1500000,
        currency: "JPY",
        direction: "outbound",
        counterparty_country: "PH",
        channel: "branch",
        executed_at: "2025-01-10T10:00:00Z",
        created_at: "2025-01-10T10:00:00Z",
      }),
    ),
  )

  await renderWithI18n(
    <MemoryRouter initialEntries={["/transactions/txn-001"]}>
      <Routes>
        <Route path="transactions/:id" element={<TransactionDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  expect((await screen.findAllByText("WIRE-001")).length).toBeGreaterThan(0)
  expect(screen.getAllByText("出金").length).toBeGreaterThan(0)
})

test("shows error state", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  await renderWithI18n(
    <MemoryRouter initialEntries={["/transactions/bad"]}>
      <Routes>
        <Route path="transactions/:id" element={<TransactionDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  expect(await screen.findByText("取引データの取得に失敗しました")).toBeDefined()
})
