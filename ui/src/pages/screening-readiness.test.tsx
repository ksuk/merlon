import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { ScreeningQueuePage } from "./screening-queue"

// ScreeningSourceStatus carries last_attempt_at, age_seconds,
// freshness_threshold_seconds and consecutive_failures, and none of them were
// rendered. #70 asks for "last attempt ... age, configured threshold": a
// source retried hourly and failing every time looked identical to an
// abandoned one, and "26 hours old" means nothing without the window it is
// judged against.

const sources = {
  data: [
    {
      list_id: "ofac_sdn",
      list_type: "sanctions",
      configured: true,
      operational_state: "stale",
      last_success_at: "2025-03-01T00:00:00Z",
      last_attempt_at: "2025-03-03T00:00:00Z",
      age_seconds: 93600,
      freshness_threshold_seconds: 259200,
      consecutive_failures: 4,
      diagnostic: "source returned 503",
    },
    {
      list_id: "never_seen",
      list_type: "pep",
      configured: true,
      operational_state: "never_imported",
      freshness_threshold_seconds: 259200,
      consecutive_failures: 0,
    },
  ],
  screening_ready: false,
  degraded_sources: ["ofac_sdn"],
  configured_count: 2,
  ready_count: 0,
  unready_count: 2,
  policy_version: "2026-08-06-default",
}

function mockAPI() {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    if (url.includes("/screening/sources")) {
      return Promise.resolve(new Response(JSON.stringify(sources)))
    }
    if (url.includes("/policies/screening_readiness")) {
      return Promise.resolve(new Response(JSON.stringify({ document: { required: ["ofac_sdn"] }, version: "v1" })))
    }
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("a stale source shows its last attempt, age against the threshold, and failure streak", async () => {
  mockAPI()
  await renderWithI18n(
    <MemoryRouter>
      <ScreeningQueuePage />
    </MemoryRouter>,
  )

  const lastAttempt = await screen.findByTestId("source-last-attempt-ofac_sdn")
  expect(lastAttempt.textContent).not.toBe("")
  expect(lastAttempt.textContent).not.toContain("未実施")

  // 93,600s is 26 hours against a 3-day window: both numbers must be present
  // or the operator cannot tell whether it is late.
  const age = screen.getByTestId("source-age-ofac_sdn")
  expect(age.textContent).toContain("26時間")
  expect(age.textContent).toContain("3日")

  expect(screen.getByTestId("source-failures-ofac_sdn").textContent).toBe("4")
})

test("a never-imported source says so rather than showing a blank cell", async () => {
  mockAPI()
  await renderWithI18n(
    <MemoryRouter>
      <ScreeningQueuePage />
    </MemoryRouter>,
  )

  const lastAttempt = await screen.findByTestId("source-last-attempt-never_seen")
  expect(lastAttempt.textContent).toContain("未実施")
  expect(screen.getByTestId("source-age-never_seen").textContent).toBe("-")
})
