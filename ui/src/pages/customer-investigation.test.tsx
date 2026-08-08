import { screen, within } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { CustomerDetailPage } from "./customer-detail"

// GET /customers/{id}/investigation returns transactions, alerts, cases,
// screening results and score history, and the frontend types every one of
// them -- but the panel rendered only the aggregate counts. An investigator
// could see that a customer had 4 alerts and had no way to reach them from
// the 360 view, which is the whole point of a 360 view.

const customer = {
  id: "c1",
  external_id: "EXT-001",
  customer_type: "individual",
  country_code: "JP",
  product_types: ["crypto"],
  attributes: { name: "Test Customer" },
  status: "active",
  risk_score: 45.2,
  risk_tier: "medium",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-15T00:00:00Z",
}

const investigation = {
  customer,
  counts: { transactions: 12, alerts: 2, cases: 1 },
  transactions: [
    {
      id: "txn-1",
      customer_id: "c1",
      external_id: "TXN-EXT-1",
      amount: 500000,
      currency: "JPY",
      direction: "inbound",
      executed_at: "2025-01-10T00:00:00Z",
      created_at: "2025-01-10T00:00:00Z",
    },
  ],
  alerts: [
    {
      id: "alert-1",
      customer_id: "c1",
      scenario_id: "structuring",
      severity: "high",
      status: "open",
      description: "Structuring pattern",
      detected_at: "2025-01-11T00:00:00Z",
      created_at: "2025-01-11T00:00:00Z",
      updated_at: "2025-01-11T00:00:00Z",
    },
  ],
  cases: [
    {
      id: "case-1",
      customer_id: "c1",
      status: "investigating",
      priority: "high",
      summary: "Linked investigation",
      alert_ids: ["alert-1"],
      created_at: "2025-01-12T00:00:00Z",
      updated_at: "2025-01-12T00:00:00Z",
    },
  ],
  screening_results: [],
  score_history: [],
  timeline: [],
  partial_failures: [],
  freshness: "2025-01-15T00:00:00Z",
}

function mockByURL(overrides: Record<string, unknown> = {}) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    for (const [fragment, body] of Object.entries(overrides)) {
      if (url.includes(fragment)) {
        return Promise.resolve(new Response(JSON.stringify(body)))
      }
    }
    if (url.includes("/investigation")) {
      return Promise.resolve(new Response(JSON.stringify(investigation)))
    }
    if (url.includes("/customers/c1") && !url.includes("/")) {
      return Promise.resolve(new Response(JSON.stringify(customer)))
    }
    if (url.match(/\/customers\/c1(\?|$)/)) {
      return Promise.resolve(new Response(JSON.stringify(customer)))
    }
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

function renderDetail() {
  return renderWithI18n(
    <MemoryRouter initialEntries={["/customers/c1"]}>
      <Routes>
        <Route path="customers/:id" element={<CustomerDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("investigation panel lists related transactions, alerts and cases with drill-down links", async () => {
  mockByURL()
  await renderDetail()

  const related = await screen.findByTestId("investigation-related")

  // Each related row must reach the full record, not just name it.
  const txnLink = within(related).getByRole("link", { name: /TXN-EXT-1|txn-1/ })
  expect(txnLink.getAttribute("href")).toBe("/transactions/txn-1")

  const alertLink = within(related).getByRole("link", { name: /structuring|alert-1/i })
  expect(alertLink.getAttribute("href")).toBe("/alerts/alert-1")

  const caseLink = within(related).getByRole("link", { name: /Linked investigation|case-1/ })
  expect(caseLink.getAttribute("href")).toBe("/cases/case-1")
})

test("related sections show status, severity and priority rather than bare identifiers", async () => {
  mockByURL()
  await renderDetail()

  const related = await screen.findByTestId("investigation-related")
  expect(within(related).getAllByText("高").length).toBeGreaterThan(0)
  expect(within(related).getAllByText("調査中").length).toBeGreaterThan(0)
})

test("a section with more records than the page links to the complete filtered list", async () => {
  mockByURL()
  await renderDetail()

  const related = await screen.findByTestId("investigation-related")
  // counts.transactions is 12 but one row was returned: the operator must be
  // able to reach the other eleven.
  const viewAll = within(related).getByTestId("investigation-view-all-transactions")
  expect(viewAll.getAttribute("href")).toBe("/transactions?customer_id=c1")
})

test("an empty related section renders an empty state, not a missing section", async () => {
  mockByURL({
    "/investigation": {
      ...investigation,
      counts: { transactions: 0, alerts: 0, cases: 0 },
      transactions: [],
      alerts: [],
      cases: [],
    },
  })
  await renderDetail()

  const related = await screen.findByTestId("investigation-related")
  expect(within(related).getAllByTestId(/investigation-empty-/).length).toBe(3)
})
