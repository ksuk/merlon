import { screen, fireEvent, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { capabilitiesFor } from "@/test/session-test-utils"
import { SessionProvider } from "@/components/session-provider"
import { RulesPage } from "./rules"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(
    <MemoryRouter>
      <SessionProvider>{ui}</SessionProvider>
    </MemoryRouter>,
  )
}

const sampleRule = {
  id: "r1",
  type: "COUNTRY_RISK",
  name: "country_risk_sample",
  description: "サンプル国別リスクテーブル",
  definition: { schema_version: "1.0" },
  version: 1,
  is_active: true,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
}

const adminUser = { id: "u1", email: "admin@example.com", role: "admin" }
const viewerUser = { id: "u2", email: "viewer@example.com", role: "viewer" }

function mockFetchRouting(user: unknown, rules: unknown[]) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/system/capabilities")) {
      const role = (user as { role?: "admin" | "analyst" | "viewer" } | null)?.role ?? "viewer"
      return Promise.resolve(new Response(JSON.stringify(capabilitiesFor({ role }))))
    }
    if (url.includes("/auth/me")) {
      return Promise.resolve(new Response(JSON.stringify(user)))
    }
    if (url.includes("/rules")) {
      return Promise.resolve(
        new Response(JSON.stringify({ data: rules, pagination: { has_more: false } })),
      )
    }
    return Promise.resolve(new Response(JSON.stringify({})))
  })
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders rule list", async () => {
  mockFetchRouting(viewerUser, [sampleRule])

  await renderWithRouter(<RulesPage />)

  expect(await screen.findByText("country_risk_sample")).toBeDefined()
  expect(screen.getAllByText("国別リスク").length).toBeGreaterThan(0)
  expect(screen.getByText("有効")).toBeDefined()
})

test("shows empty state", async () => {
  mockFetchRouting(viewerUser, [])

  await renderWithRouter(<RulesPage />)

  expect(await screen.findByText("ルールが登録されていません")).toBeDefined()
})

test("hides create/import actions for non-admin roles", async () => {
  mockFetchRouting(viewerUser, [sampleRule])

  await renderWithRouter(<RulesPage />)

  await screen.findByText("country_risk_sample")
  expect(screen.queryByText("新規作成")).toBeNull()
  expect(screen.queryByText("インポート")).toBeNull()
})

test("filters by type by calling the API with the type query param", async () => {
  mockFetchRouting(adminUser, [sampleRule])
  const fetchSpy = vi.spyOn(globalThis, "fetch")

  await renderWithRouter(<RulesPage />)
  await screen.findByText("country_risk_sample")

  fireEvent.click(screen.getByText("CDD重み付け"))

  await waitFor(() => {
    const calledWithCddFilter = fetchSpy.mock.calls.some(([input]) => {
      const url = typeof input === "string" ? input : String(input)
      return url.includes("/rules?") && url.includes("type=CDD_WEIGHT")
    })
    expect(calledWithCddFilter).toBe(true)
  })
})

test("submits the import form", async () => {
  mockFetchRouting(adminUser, [])
  const fetchSpy = vi.spyOn(globalThis, "fetch")

  await renderWithRouter(<RulesPage />)
  await screen.findByText("ルールが登録されていません")

  fireEvent.click(screen.getByText("インポート"))

  const textarea = await screen.findByPlaceholderText(/type.*COUNTRY_RISK/)
  fireEvent.change(textarea, {
    target: {
      value: JSON.stringify([
        { type: "COUNTRY_RISK", name: "imported_rule", definition: { schema_version: "1.0" } },
      ]),
    },
  })

  const submitButtons = screen.getAllByText("インポート")
  fireEvent.click(submitButtons[submitButtons.length - 1])

  await waitFor(() => {
    const calledImport = fetchSpy.mock.calls.some(([input]) => {
      const url = typeof input === "string" ? input : String(input)
      return url.includes("/rules/import")
    })
    expect(calledImport).toBe(true)
  })
})

test("export button links to the export download endpoint", async () => {
  mockFetchRouting(adminUser, [sampleRule])

  await renderWithRouter(<RulesPage />)
  await screen.findByText("country_risk_sample")

  const link = screen.getByText("エクスポート").closest("a")
  expect(link?.getAttribute("href")).toContain("/rules/country_risk_sample/export")
})
