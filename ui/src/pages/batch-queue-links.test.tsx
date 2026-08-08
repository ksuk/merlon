import { screen, within } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { BatchPage } from "./batch"
import { PendingEvaluationsPage } from "./pending-evaluations"

// #72 asks the preview to display the excluded/ineligible count and expected
// side effects; #73 asks the pending queue to show backlog, oldest age and
// exhausted counts as stop conditions; #74 asks run history to be filterable
// and a run to be cancellable. All of the data existed by the time these
// pages rendered it -- none of it was on screen.

const manifest = {
  id: "manifest-1",
  operation: "batch_monitor",
  target_mode: "all",
  customer_ids: ["cust-1"],
  sample_customer_ids: ["cust-1"],
  target_count: 1,
  excluded_count: 2,
  excluded_reasons: { closed: 1, dormant: 1 },
  expected_side_effects: [
    "evaluates transaction monitoring scenarios for every target",
    "may create alerts, and may consolidate new alerts into cases",
  ],
  rule_set_id: "tm_default",
  rule_set_version: 3,
  criteria: "{}",
  token: "tok",
  status: "preview",
  version: 1,
  expires_at: "2099-01-01T00:00:00Z",
  created_at: "2025-01-01T00:00:00Z",
}

const runningRun = {
  id: "run-9",
  job_type: "batch_monitor",
  operation: "batch_monitor",
  status: "running",
  parameters: {},
  target_manifest_id: "manifest-1",
  config_digests: {},
  actor: "analyst",
  result_counts: { total: 3, queued_for_review: 2 },
  started_at: "2025-01-01T00:00:00Z",
  processed_customer_ids: [],
  updated_at: "2025-01-01T00:00:00Z",
}

const stats = {
  backlog: 7,
  by_status: { PENDING_REVIEW: 6, FAILED: 1 },
  failed: 1,
  exhausted: 3,
  oldest_created_at: "2025-01-01T00:00:00Z",
  oldest_age_seconds: 93600,
  evaluated_at: "2025-01-04T02:00:00Z",
}

const requestedURLs: string[] = []

function mockAPI(extra: Record<string, unknown> = {}) {
  requestedURLs.length = 0
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    requestedURLs.push(url)
    for (const [fragment, body] of Object.entries(extra)) {
      if (url.includes(fragment)) return Promise.resolve(new Response(JSON.stringify(body)))
    }
    if (url.includes("/batch/targets/preview")) return Promise.resolve(new Response(JSON.stringify(manifest)))
    if (url.includes("/pending-evaluations/stats")) return Promise.resolve(new Response(JSON.stringify(stats)))
    if (url.includes("/batch/runs/run-9")) return Promise.resolve(new Response(JSON.stringify(runningRun)))
    if (url.includes("/batch/runs")) return Promise.resolve(new Response(JSON.stringify({ data: [runningRun], pagination: { has_more: false } })))
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("run history can be filtered by status and operation", async () => {
  mockAPI()
  await renderWithI18n(
    <MemoryRouter initialEntries={["/batch"]}>
      <Routes>
        <Route path="batch" element={<BatchPage />} />
      </Routes>
    </MemoryRouter>,
  )

  const status = (await screen.findByTestId("batch-run-history")).querySelector("#run-status-filter")
  const operation = screen.getByTestId("batch-run-history").querySelector("#run-operation-filter")
  expect(status).not.toBeNull()
  expect(operation).not.toBeNull()
})

test("following a pending record's batch link shows which run it was about", async () => {
  mockAPI()
  await renderWithI18n(
    <MemoryRouter initialEntries={["/batch?run=run-9"]}>
      <Routes>
        <Route path="batch" element={<BatchPage />} />
      </Routes>
    </MemoryRouter>,
  )

  const linked = await screen.findByTestId("batch-linked-run")
  expect(linked.textContent).toContain("run-9")
})

test("the pending queue reports backlog, oldest age and exhausted count", async () => {
  mockAPI()
  await renderWithI18n(
    <MemoryRouter>
      <PendingEvaluationsPage />
    </MemoryRouter>,
  )

  const panel = await screen.findByTestId("pending-stats")
  expect(within(panel).getByTestId("pending-backlog").textContent).toBe("7")
  expect(within(panel).getByTestId("pending-exhausted").textContent).toBe("3")
  // 93,600 seconds is 26 hours; a raw second count is not a stop condition.
  expect(within(panel).getByTestId("pending-oldest").textContent).toContain("26時間")
})

test("an empty queue shows no oldest age rather than an age of zero", async () => {
  mockAPI({
    "/pending-evaluations/stats": {
      backlog: 0,
      by_status: {},
      failed: 0,
      exhausted: 0,
      oldest_created_at: null,
      oldest_age_seconds: 0,
      evaluated_at: "2025-01-04T02:00:00Z",
    },
  })
  await renderWithI18n(
    <MemoryRouter>
      <PendingEvaluationsPage />
    </MemoryRouter>,
  )

  const panel = await screen.findByTestId("pending-stats")
  expect(within(panel).getByTestId("pending-oldest").textContent).toBe("-")
})
