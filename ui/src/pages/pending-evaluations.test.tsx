import { fireEvent, screen, waitFor } from "@testing-library/react"
import { beforeEach, expect, test, vi } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { paginatedResponse } from "@/test/api-test-utils"
import { PendingEvaluationsPage } from "./pending-evaluations"

beforeEach(() => {
  vi.restoreAllMocks()
})

test("opens a pending evaluation row with the keyboard and keeps the action form available", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.includes("/pending-evaluations?") && !url.includes("/history")) {
      return paginatedResponse([{
        id: "pending-1",
        customer_id: "cust-1",
        transaction_ids: ["txn-1"],
        status: "PENDING_REVIEW",
        reason: "engine unavailable",
        retry_count: 1,
        version: 2,
        created_at: "2026-08-05T00:00:00Z",
        updated_at: "2026-08-05T00:00:00Z",
      }])
    }
    if (url.includes("/pending-evaluations/pending-1/history")) {
      return new Response(JSON.stringify([]), { status: 200, headers: { "Content-Type": "application/json" } })
    }
    throw new Error(`unexpected request: ${url}`)
  })

  renderWithI18n(<MemoryRouter><PendingEvaluationsPage /></MemoryRouter>)

  const row = await screen.findByRole("button", { name: /cust-1/ })
  fireEvent.keyDown(row, { key: "Enter" })
  expect(await screen.findByLabelText("操作理由")).toBeDefined()
  expect(screen.getAllByText("engine unavailable").length).toBeGreaterThan(0)
})

test("opens the exact pending evaluation named by the transaction deep link", async () => {
  const pending = {
    id: "pending-1",
    customer_id: "cust-1",
    transaction_ids: ["txn-1"],
    status: "PENDING_REVIEW",
    reason: "monitoring unavailable",
    retry_count: 0,
    version: 1,
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
  }
  let exactFetches = 0
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/pending-evaluations/pending-1")) {
      exactFetches += 1
      return new Response(JSON.stringify(pending), { status: 200, headers: { "Content-Type": "application/json" } })
    }
    if (url.includes("/pending-evaluations/pending-1/history")) {
      return new Response(JSON.stringify([]), { status: 200, headers: { "Content-Type": "application/json" } })
    }
    if (url.includes("/pending-evaluations?") && !url.includes("/history")) {
      return paginatedResponse([pending])
    }
    if (url.includes("/pending-evaluations/stats")) {
      return new Response(JSON.stringify({ backlog: 1, by_status: { PENDING_REVIEW: 1 }, failed: 0, exhausted: 0, oldest_created_at: pending.created_at, oldest_age_seconds: 0, evaluated_at: pending.created_at }))
    }
    throw new Error(`unexpected request: ${url}`)
  })

  renderWithI18n(
    <MemoryRouter initialEntries={["/pending-evaluations?pending_evaluation_id=pending-1"]}>
      <PendingEvaluationsPage />
    </MemoryRouter>,
  )

  expect(await screen.findByLabelText("操作理由")).toBeDefined()
  expect(screen.getAllByText("monitoring unavailable").length).toBeGreaterThan(0)
  expect(exactFetches).toBe(1)

  fireEvent.click(screen.getByRole("button", { name: "更新" }))
  await waitFor(() => expect(exactFetches).toBe(2))
})
