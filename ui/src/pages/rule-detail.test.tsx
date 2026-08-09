import { fireEvent, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, expect, test, vi } from "vitest"
import { SessionProvider } from "@/components/session-provider"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { capabilitiesFor } from "@/test/session-test-utils"
import { RuleDetailPage } from "./rule-detail"

beforeEach(() => {
  vi.restoreAllMocks()
})

const v3 = {
  id: "row-3",
  type: "TM_SCENARIO",
  name: "tm_structuring_basic",
  description: "Structuring detection",
  definition: { threshold: 1000000, window_days: 7 },
  version: 3,
  is_active: true,
  created_by: "author@example.com",
  created_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-02T00:00:00Z",
}

const v2 = { ...v3, id: "row-2", version: 2, definition: { threshold: 500000, window_days: 7 }, is_active: false }

function mockRuleAPI(byVersion: Record<number, unknown> = { 2: v2 }) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/system/capabilities")) {
      return Promise.resolve(new Response(JSON.stringify(capabilitiesFor({ role: "admin" }))))
    }
    const version = new URL(url, "http://localhost").searchParams.get("version")
    if (version) {
      const body = byVersion[Number(version)]
      if (!body) return Promise.resolve(new Response(JSON.stringify({ error: "not found" }), { status: 404 }))
      return Promise.resolve(new Response(JSON.stringify(body)))
    }
    if (url.includes("/rules/")) {
      return Promise.resolve(new Response(JSON.stringify(v3)))
    }
    return Promise.resolve(new Response(JSON.stringify({})))
  })
}

function renderDetail() {
  return renderWithI18n(
    <MemoryRouter initialEntries={["/rules/tm_structuring_basic"]}>
      <SessionProvider>
        <Routes>
          <Route path="rules/:name" element={<RuleDetailPage />} />
        </Routes>
      </SessionProvider>
    </MemoryRouter>,
  )
}

test("shows the canonical identifier and the effective definition", async () => {
  mockRuleAPI()

  await renderDetail()

  expect(await screen.findByText("tm_structuring_basic @ v3")).toBeDefined()
  // The rule card list showed only name, type, status and version; the
  // thresholds that decide what actually fires were unreachable.
  expect(screen.getAllByText("threshold").length).toBeGreaterThan(0)
  expect(screen.getAllByText("1000000").length).toBeGreaterThan(0)
  expect(screen.getByText("Structuring detection")).toBeDefined()
})

test("compares against the previous version by default and names what changed", async () => {
  mockRuleAPI()

  await renderDetail()

  await screen.findByText("tm_structuring_basic @ v3")
  // window_days is identical in both versions, so it must not appear as a
  // change; threshold must.
  expect(await screen.findByText("threshold", { selector: "div" })).toBeDefined()
  expect(screen.getAllByText("500000").length).toBeGreaterThan(0)
})

test("reports an unretrievable version without failing the page", async () => {
  mockRuleAPI({})

  await renderDetail()

  await screen.findByText("tm_structuring_basic @ v3")
  fireEvent.click(screen.getByRole("button", { name: "v1" }))

  expect(await screen.findByText("そのバージョンは取得できません。")).toBeDefined()
  // The definition is still worth reading even when a comparison is not.
  expect(screen.getAllByText("1000000").length).toBeGreaterThan(0)
})

test("exposes the selected comparison version to assistive technology", async () => {
  mockRuleAPI()

  await renderDetail()

  await screen.findByText("tm_structuring_basic @ v3")
  expect(screen.getByRole("button", { name: "v2" }).getAttribute("aria-pressed")).toBe("true")
  expect(screen.getByRole("button", { name: "v1" }).getAttribute("aria-pressed")).toBe("false")
})
