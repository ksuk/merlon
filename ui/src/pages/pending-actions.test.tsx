import { fireEvent, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { PendingEvaluationsPage } from "./pending-evaluations"

// The pending queue had one test, covering keyboard row selection. None of the
// recovery actions were exercised, even though each one moves a monitoring gap
// between states and every one of them must carry the record's expected
// version or it can overwrite a concurrent decision.

const record = {
  id: "pe-1",
  customer_id: "cust-1",
  transaction_ids: ["txn-1", "txn-2"],
  status: "PENDING_REVIEW",
  reason: "engine unavailable: dial tcp: connection refused",
  retry_count: 2,
  manual_retry_count: 0,
  version: 4,
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-02T00:00:00Z",
}

const stats = {
  backlog: 1,
  by_status: { PENDING_REVIEW: 1 },
  failed: 0,
  exhausted: 0,
  oldest_created_at: "2025-01-01T00:00:00Z",
  oldest_age_seconds: 7200,
  evaluated_at: "2025-01-01T02:00:00Z",
}

const posted: { url: string; body: Record<string, unknown> | null }[] = []

function mockAPI() {
  posted.length = 0
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    if (init?.method && init.method !== "GET") {
      posted.push({ url, body: init.body ? JSON.parse(String(init.body)) : null })
      return Promise.resolve(new Response(JSON.stringify({ ...record, status: "RESOLVED", version: 5 })))
    }
    if (url.includes("/pending-evaluations/stats")) {
      return Promise.resolve(new Response(JSON.stringify(stats)))
    }
    if (url.includes("/history")) {
      return Promise.resolve(new Response(JSON.stringify([])))
    }
    if (url.includes("/pending-evaluations")) {
      return Promise.resolve(new Response(JSON.stringify({ data: [record], pagination: { has_more: false } })))
    }
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

async function openDetail() {
  await renderWithI18n(
    <MemoryRouter>
      <PendingEvaluationsPage />
    </MemoryRouter>,
  )
  fireEvent.click(await screen.findByRole("button", { name: /cust-1/ }))
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("the detail panel shows the failure reason and affected transaction count", async () => {
  mockAPI()
  await openDetail()

  // The safe failure summary is what an operator decides on; hiding it leaves
  // "retry" as a guess.
  // Shown in both the row and the detail panel; either alone would do.
  expect((await screen.findAllByText(/connection refused/)).length).toBeGreaterThan(0)
  expect(screen.getAllByText(/2/).length).toBeGreaterThan(0)
})

test.each([
  ["再試行", "retry"],
  ["解決", "resolve"],
  ["エスカレーション", "escalate"],
])("%s sends the record's expected version", async (label, action) => {
  mockAPI()
  await openDetail()

  fireEvent.change(await screen.findByLabelText(/理由/), { target: { value: "operator note" } })
  fireEvent.click(screen.getByRole("button", { name: new RegExp(label) }))

  await vi.waitFor(() => {
    const call = posted.find((entry) => entry.url.includes(`/pending-evaluations/pe-1/${action}`))
    expect(call).toBeDefined()
    // Without expected_version a stale panel silently overwrites whatever
    // another operator decided in the meantime.
    expect(call!.body?.expected_version).toBe(4)
  })
})
