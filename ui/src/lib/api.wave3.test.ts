import { beforeEach, expect, test, vi } from "vitest"
import { api } from "./api"

// api.test.ts covers the Wave 1 and Wave 2 contracts. The Wave 3 clients --
// transaction-scoped queues, the cohort preview, the backlog aggregate, the
// run controls -- had none, so a query parameter could be renamed or dropped
// and every test would still pass.

let requests: { url: string; method: string; body: unknown }[] = []

function mockFetch(body: unknown = { data: [], pagination: { has_more: false } }) {
  requests = []
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    requests.push({ url, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : null })
    return Promise.resolve(new Response(JSON.stringify(body)))
  })
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("alerts and cases are scoped by transaction_id server-side", async () => {
  mockFetch()
  await api.alerts.list({ transactionId: "txn-1" })
  await api.cases.list({ transactionId: "txn-1" })

  // Filtering a customer's page client side loses records once the customer
  // has more alerts than one page; the scoping has to reach the server.
  expect(requests[0].url).toContain("transaction_id=txn-1")
  expect(requests[1].url).toContain("transaction_id=txn-1")
})

test("the backtest cohort preview posts before any job exists", async () => {
  mockFetch({ customer_count: 0, transaction_count: 0, transaction_counted: true, sample_customer_ids: [], empty: true, warnings: ["no customers"] })
  const preview = await api.backtest.previewCohort({ customer_ids: ["c1"] })

  expect(requests[0].url).toContain("/backtests/preview")
  expect(requests[0].method).toBe("POST")
  expect(preview.empty).toBe(true)
})

test("the pending backlog is read from the stats endpoint, not counted from a page", async () => {
  mockFetch({ backlog: 12, by_status: {}, failed: 2, exhausted: 1, oldest_created_at: null, oldest_age_seconds: 0, evaluated_at: "2025-01-01T00:00:00Z" })
  const stats = await api.pending.stats()

  expect(requests[0].url).toContain("/pending-evaluations/stats")
  expect(stats.backlog).toBe(12)
  // null, not 0: an empty queue must not read as "something just arrived".
  expect(stats.oldest_created_at).toBeNull()
})

test("a running batch can be cancelled", async () => {
  mockFetch({ id: "run-1", status: "cancelled" })
  await api.batch.cancel("run-1")

  expect(requests[0].url).toContain("/batch/runs/run-1/cancel")
  expect(requests[0].method).toBe("POST")
})

test("batch run history carries the status and operation filters and a cursor", async () => {
  mockFetch()
  await api.batch.runs({ status: "failed", operation: "batch_monitor", cursor: "abc", limit: 20 })

  const url = requests[0].url
  expect(url).toContain("status=failed")
  expect(url).toContain("operation=batch_monitor")
  expect(url).toContain("cursor=abc")
})

test("screening results can be paged and can include suppressed hits on request", async () => {
  mockFetch()
  await api.screening.results({ suppressed: false, cursor: "page2", limit: 50 })

  const url = requests[0].url
  expect(url).toContain("suppressed=false")
  expect(url).toContain("cursor=page2")
})
