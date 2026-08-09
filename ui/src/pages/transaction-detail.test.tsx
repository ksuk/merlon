import { screen, within } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { TransactionDetailPage } from "./transaction-detail"

// The rewritten transaction detail page had no test at all. These cover the
// four defects #78 leaves behind: a link to a page that does not exist, a raw
// country code where every other screen localises one, related records
// reduced to bare identifiers, and a related-record list derived by filtering
// one page of an unrelated payload.

const txn = {
  id: "txn-1",
  customer_id: "c1",
  external_id: "TXN-EXT-1",
  account_id: "acct-9",
  amount: 900000,
  currency: "JPY",
  direction: "outbound",
  counterparty_country: "SG",
  executed_at: "2025-02-01T00:00:00Z",
  created_at: "2025-02-01T00:00:00Z",
  travel_rule_applicable: true,
  travel_rule_status: "complete",
}

const alerts = {
  data: [
    {
      id: "alert-1",
      customer_id: "c1",
      scenario_id: "high_risk_country_transfer",
      severity: "critical",
      status: "investigating",
      description: "Transfer to a high risk jurisdiction",
      transaction_ids: ["txn-1"],
      detected_at: "2025-02-01T01:00:00Z",
      created_at: "2025-02-01T01:00:00Z",
      updated_at: "2025-02-01T01:00:00Z",
    },
  ],
  pagination: { has_more: false },
}

const cases = {
  data: [
    {
      id: "case-1",
      customer_id: "c1",
      status: "escalated",
      priority: "critical",
      summary: "Cross-border review",
      alert_ids: ["alert-1"],
      created_at: "2025-02-01T02:00:00Z",
      updated_at: "2025-02-01T02:00:00Z",
    },
  ],
  pagination: { has_more: false },
}

const requestedURLs: string[] = []

function mockAPI() {
  requestedURLs.length = 0
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    requestedURLs.push(url)
    if (url.includes("/transactions/txn-1")) {
      return Promise.resolve(new Response(JSON.stringify(txn)))
    }
    if (url.includes("/alerts")) {
      return Promise.resolve(new Response(JSON.stringify(alerts)))
    }
    if (url.includes("/cases")) {
      return Promise.resolve(new Response(JSON.stringify(cases)))
    }
    if (url.includes("/policies/travel_rule")) {
      return Promise.resolve(new Response(JSON.stringify({ document: {}, version: "v1" })))
    }
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

function renderDetail() {
  return renderWithI18n(
    <MemoryRouter initialEntries={["/transactions/txn-1"]}>
      <Routes>
        <Route path="transactions/:id" element={<TransactionDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("the account identifier is not a link to a page that does not exist", async () => {
  mockAPI()
  await renderDetail()

  const account = await screen.findByTestId("transaction-account-id")
  expect(account.textContent).toContain("acct-9")
  // There is no /accounts route in App.tsx, so a link here lands on NotFound.
  expect(account.querySelector("a")).toBeNull()
})

test("the counterparty country is localized rather than a raw ISO code", async () => {
  mockAPI()
  await renderDetail()

  const country = await screen.findByTestId("transaction-counterparty-country")
  expect(country.textContent).toContain("シンガポール")
})

test("related alerts and cases are fetched by transaction, not filtered from a customer page", async () => {
  mockAPI()
  await renderDetail()

  await screen.findByTestId("transaction-related")
  expect(requestedURLs.some((url) => url.includes("/alerts") && url.includes("transaction_id=txn-1"))).toBe(true)
  expect(requestedURLs.some((url) => url.includes("/cases") && url.includes("transaction_id=txn-1"))).toBe(true)
})

test("related records carry severity, status and priority", async () => {
  mockAPI()
  await renderDetail()

  const related = await screen.findByTestId("transaction-related")
  expect(within(related).getAllByText("重大").length).toBeGreaterThan(0)
  expect(within(related).getByText("調査中")).toBeDefined()
  expect(within(related).getByText("エスカレーション")).toBeDefined()
})
