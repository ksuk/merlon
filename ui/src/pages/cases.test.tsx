import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { CasesPage } from "./cases"
import { paginatedResponse } from "@/test/api-test-utils"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

function makeCase(id: string, status: string) {
  return {
    id,
    customer_id: "c1",
    alert_ids: [],
    status,
    priority: "medium",
    summary: `${status} case`,
    created_at: "2025-01-15T10:00:00Z",
    updated_at: "2025-01-15T10:00:00Z",
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("displays new/reopened/str_filed status badges", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    paginatedResponse([
        makeCase("case-new", "new"),
        makeCase("case-reopened", "reopened"),
        makeCase("case-str", "str_filed"),
      ]),
  )

  await renderWithRouter(<CasesPage />)

  expect(await screen.findByText("再オープン")).toBeDefined()
  expect(screen.getByText("STR対象")).toBeDefined()
  expect(screen.getAllByText("新規").length).toBeGreaterThan(0)
})

test("loads a case from the second cursor page", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).includes("cursor=page-2")) {
      return paginatedResponse([makeCase("case-second", "investigating")])
    }
    return new Response(JSON.stringify({
      data: [makeCase("case-first", "new")],
      pagination: { has_more: true, next_cursor: "page-2" },
    }), { status: 200, headers: { "Content-Type": "application/json" } })
  })

  await renderWithRouter(<CasesPage />)

  expect(await screen.findByText("investigating case")).toBeDefined()
  expect(fetchMock.mock.calls.some(([input]) => String(input).includes("cursor=page-2"))).toBe(true)
})
