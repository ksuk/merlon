import { screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, expect, test, vi } from "vitest"
import { axe } from "vitest-axe"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { AlertsPage } from "./alerts"
import { CasesPage } from "./cases"

/**
 * Automated accessibility checks for the operator screens #86 names.
 *
 * These run axe over the rendered page, which catches the class of defect a
 * hand-written assertion tends to miss: a control with no accessible name, a
 * duplicated id, a table cell outside a row. They complement rather than
 * replace the targeted semantic assertions in the page tests.
 */

beforeEach(() => {
  vi.restoreAllMocks()
})

function mockList(rows: unknown[]) {
  vi.spyOn(globalThis, "fetch").mockImplementation(() =>
    Promise.resolve(new Response(JSON.stringify({ data: rows, pagination: { has_more: false } }))),
  )
}

const alertRow = {
  id: "a1",
  customer_id: "c1",
  scenario_id: "tm_structuring_basic",
  severity: "high",
  status: "open",
  score: 80,
  description: "detected",
  transaction_ids: [],
  detected_at: "2026-08-01T00:00:00Z",
  created_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-01T00:00:00Z",
}

const caseRow = {
  id: "k1",
  customer_id: "c1",
  status: "open",
  priority: "high",
  alert_ids: [],
  created_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-01T00:00:00Z",
}

test("the alert queue has no automatically detectable accessibility violations", async () => {
  mockList([alertRow])

  const { container } = await renderWithI18n(
    <MemoryRouter>
      <AlertsPage />
    </MemoryRouter>,
  )
  await screen.findByText("tm_structuring_basic")

  expect(await axe(container)).toHaveNoViolations()
})

test("the case queue has no automatically detectable accessibility violations", async () => {
  mockList([caseRow])

  const { container } = await renderWithI18n(
    <MemoryRouter>
      <CasesPage />
    </MemoryRouter>,
  )

  expect(await axe(container)).toHaveNoViolations()
})

// A row that looks clickable and is not wastes an operator's attention on every
// pass down the list. The fix was to stop advertising an interaction that does
// not exist, so this guards against it being reintroduced.
test("table rows do not advertise a click they do not handle", async () => {
  mockList([alertRow])

  const { container } = await renderWithI18n(
    <MemoryRouter>
      <AlertsPage />
    </MemoryRouter>,
  )
  await screen.findByText("tm_structuring_basic")

  for (const row of container.querySelectorAll("tbody tr")) {
    const claimsPointer = row.className.includes("cursor-pointer")
    const handlesActivation = row.getAttribute("role") === "button" || row.hasAttribute("tabindex")
    if (claimsPointer && !handlesActivation) {
      throw new Error("a table row shows a pointer cursor but is not operable")
    }
  }
})
