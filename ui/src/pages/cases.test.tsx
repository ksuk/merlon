import { fireEvent, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { CasesPage } from "./cases"
import { paginatedResponse } from "@/test/api-test-utils"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

function makeCase(id: string, status: string, priority = "medium", strCandidate = false) {
  return {
    id,
    customer_id: "c1",
    alert_ids: [],
    status,
    priority,
    str_candidate: strCandidate,
    summary: `${status} case`,
    created_at: "2025-01-15T10:00:00Z",
    updated_at: "2025-01-15T10:00:00Z",
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("displays new/reopened/str_filed status badges", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([
    makeCase("case-new", "new", "medium", true),
    makeCase("case-reopened", "reopened"),
    makeCase("case-str", "str_filed"),
  ]))

  await renderWithRouter(<CasesPage />)

  await screen.findByText("reopened case")
  const rows = screen.getAllByRole("row").slice(1)
  expect(rows.some((row) => row.textContent?.includes("再オープン"))).toBe(true)
  expect(rows.some((row) => row.textContent?.includes("STR届出済み"))).toBe(true)
  expect(rows.some((row) => row.textContent?.includes("STR対象"))).toBe(true)
  expect(rows.filter((row) => row.textContent?.includes("新規")).length).toBeGreaterThan(0)
})

test("loads a case from the second cursor page", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
    return new Response(JSON.stringify({
      data: [makeCase("case-first", "new")],
      pagination: { has_more: true, next_cursor: "page-2" },
    }), { status: 200, headers: { "Content-Type": "application/json" } })
  })

  await renderWithRouter(<CasesPage />)

  expect(await screen.findByText("new case")).toBeDefined()
  const next = screen.getByRole("button", { name: "次へ" })
  fetchMock.mockImplementation(async (input) => {
    if (String(input).includes("cursor=page-2")) {
      return paginatedResponse([makeCase("case-second", "investigating")])
    }
    return paginatedResponse([makeCase("case-first", "new")])
  })
  fireEvent.click(next)
  expect(await screen.findByText("investigating case")).toBeDefined()
  expect(fetchMock.mock.calls.some(([input]) => String(input).includes("cursor=page-2"))).toBe(true)
})

test("renders critical cases before lower-priority cases", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([
    makeCase("case-low", "new", "low"),
    makeCase("case-critical", "new", "critical"),
  ]))

  await renderWithRouter(<CasesPage />)

  await screen.findAllByText("new case")
  const rows = screen.getAllByRole("row").slice(1)
  expect(rows[0]).toHaveTextContent("重大")
  expect(rows[1]).toHaveTextContent("低")
})

test("restores URL queue filters and preserves priority/unassigned/SLA controls", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => paginatedResponse([
    { ...makeCase("case-filtered", "investigating", "critical"), assigned_to: "", due_at: "2025-01-01T00:00:00Z" },
  ]))

  await renderWithI18n(
    <MemoryRouter initialEntries={["/cases?priority=critical&unassigned=true&overdue=true&search=customer-1&str_candidate=true"]}>
      <CasesPage />
    </MemoryRouter>,
  )

  await screen.findByText("investigating case")
  expect(screen.getByLabelText("優先度")).toHaveValue("critical")
  expect(screen.getByRole("checkbox", { name: "未割当" })).toBeChecked()
  expect(screen.getByRole("checkbox", { name: "期限超過" })).toBeChecked()
  expect(screen.getByLabelText("検索")).toHaveValue("customer-1")
  expect(screen.getByLabelText("STR候補")).toHaveValue("true")
  const queueRequest = fetchMock.mock.calls.find(([input]) => String(input).includes("/cases?"))
  expect(String(queueRequest?.[0])).toContain("priority=critical")
  expect(String(queueRequest?.[0])).toContain("unassigned=true")
  expect(String(queueRequest?.[0])).toContain("overdue=true")
  expect(String(queueRequest?.[0])).toContain("str_candidate=true")
})
