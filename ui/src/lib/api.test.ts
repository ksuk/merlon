import { beforeEach, expect, test, vi } from "vitest"
import { api, ApiError } from "./api"

beforeEach(() => {
  vi.restoreAllMocks()
})

test("request throws ApiError with the server's error_code and message", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ error: "customer not found", error_code: "not_found" }), { status: 404 }),
  )

  await expect(api.customers.get("nonexistent")).rejects.toMatchObject({
    name: "ApiError",
    status: 404,
    code: "not_found",
    message: "customer not found",
  })
})

test("request falls back to the raw body when the error response is not JSON", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("upstream timeout", { status: 504 }))

  await expect(api.customers.get("x")).rejects.toMatchObject({
    name: "ApiError",
    status: 504,
    code: undefined,
    message: "upstream timeout",
  })
})

test("ApiError is an instance of Error", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ error: "forbidden", error_code: "forbidden" }), { status: 403 }),
  )

  try {
    await api.customers.get("x")
    throw new Error("expected rejection")
  } catch (err) {
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toBeInstanceOf(Error)
  }
})

// Every list endpoint returns the {"data": [...], "pagination": {...}} envelope
// the HTTP API contract §1.1 specifies (writePaginatedJSON in
// api/internal/server/helpers.go). request<T> hands back res.json() unwrapped,
// so a client method that declares a bare array does not fail loudly -- it
// hands the page an object with no .filter, .map or .length and the list view
// renders empty against a real server while its tests pass.
//
// The guard here is the type check, not the runtime assertions: types are
// erased, so `page.data` below only compiles while the declaration is honest.
// Re-declaring any of these as an array fails `npm run build` in this file.
// The runtime assertions pin the wire shape these declarations claim.
test.each([
  ["customers", () => api.customers.list()],
  ["alerts", () => api.alerts.list()],
  ["cases", () => api.cases.list()],
  ["transactions", () => api.transactions.list("c1")],
])("%s.list returns the paginated envelope, not a bare array", async (_name, list) => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({ data: [{ id: "1" }], pagination: { has_more: false } }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    ),
  )

  const page = await list()

  expect(Array.isArray(page.data)).toBe(true)
  expect(page.data).toHaveLength(1)
  expect(page.pagination.has_more).toBe(false)
})

test("transactions.list always sends its required customer scope", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ data: [], pagination: { has_more: false } }), { status: 200 }),
  )

  await api.transactions.list("c1")

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/transactions?customer_id=c1",
    expect.objectContaining({ headers: { "Content-Type": "application/json" } }),
  )
})

// The counterpart: these are served with writeJSON, not writePaginatedJSON, so
// declaring them as envelopes would break them the same way in reverse.
test.each([
  ["customers.scoreHistory", () => api.customers.scoreHistory("c1")],
  ["cases.related", () => api.cases.related("k1")],
])("%s returns a bare array", async (_name, fetchList) => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify([{ id: "1" }]), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  )

  expect(Array.isArray(await fetchList())).toBe(true)
})

test("customers.screen normalizes legacy null matches", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        customer_id: "c1",
        hit: false,
        matches: null,
        lists_checked: 2,
        screened_at: "2026-08-02T00:00:00Z",
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    ),
  )

  const result = await api.customers.screen("c1", [])

  expect(result.matches).toEqual([])
})

test("backtest.get normalizes legacy null scenario results", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        id: "job-1",
        status: "completed",
        candidate: {
          backtest_id: "backtest-1",
          total_transactions: 0,
          total_customers: 0,
          total_alerts: 0,
          scenario_results: null,
          execution_time_ms: 0,
        },
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    ),
  )

  const job = await api.backtest.get("job-1")

  expect(job.candidate?.scenario_results).toEqual([])
})
