import { fireEvent, screen, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { capabilitiesFor } from "@/test/session-test-utils"
import { SessionProvider } from "@/components/session-provider"
import { api } from "@/lib/api"
import { CustomerDetailPage } from "./customer-detail"

function renderWithRoute(id: string) {
  return renderWithI18n(
    <MemoryRouter initialEntries={[`/customers/${id}`]}>
      <SessionProvider>
        <Routes>
          <Route path="customers/:id" element={<CustomerDetailPage />} />
        </Routes>
      </SessionProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders customer detail with profile data", async () => {
  let callCount = 0
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/system/capabilities")) {
      return Promise.resolve(new Response(JSON.stringify(capabilitiesFor({ role: "admin" }))))
    }
    if (url.includes("/auth/me")) {
      return Promise.resolve(
        new Response(JSON.stringify({ id: "u1", email: "a@example.com", role: "admin" })),
      )
    }
    callCount++
    if (callCount === 1) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            id: "c1",
            external_id: "EXT-001",
            customer_type: "individual",
            country_code: "JP",
            product_types: ["crypto"],
            attributes: { name: "Test" },
            risk_score: 45.2,
            risk_tier: "medium",
            last_scored_at: "2025-01-15T00:00:00Z",
            created_at: "2025-01-01T00:00:00Z",
            updated_at: "2025-01-15T00:00:00Z",
          }),
        ),
      )
    }
    return Promise.resolve(new Response(JSON.stringify([])))
  })

  await renderWithRoute("c1")

  expect(await screen.findByText("EXT-001")).toBeDefined()
  expect(screen.getByText("個人")).toBeDefined()
  expect(screen.getAllByText("中リスク").length).toBeGreaterThan(0)
  expect(screen.getByText("スコアリング")).toBeDefined()
})

test("shows error for missing customer", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("not found"))

  await renderWithRoute("nonexistent")

  expect(await screen.findByText("顧客データの取得に失敗しました")).toBeDefined()
})

test("renders an unscored customer without requesting a score explanation", async () => {
  const requested: string[] = []
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    requested.push(url)
    if (url.includes("/system/capabilities")) {
      return Promise.resolve(new Response(JSON.stringify(capabilitiesFor({ role: "admin" }))))
    }
    if (url.includes("/auth/me")) {
      return Promise.resolve(
        new Response(JSON.stringify({ id: "u1", email: "a@example.com", role: "admin" })),
      )
    }
    if (url.endsWith("/customers/c1")) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            id: "c1",
            external_id: "EXT-UNSCORED",
            customer_type: "individual",
            country_code: "JP",
            product_types: [],
            attributes: {},
            created_at: "2025-01-01T00:00:00Z",
            updated_at: "2025-01-01T00:00:00Z",
          }),
        ),
      )
    }
    if (url.includes("/score-explanation")) {
      return Promise.resolve(new Response(JSON.stringify({ error: "score record not found" }), { status: 404 }))
    }
    return Promise.resolve(new Response(JSON.stringify([])))
  })

  await renderWithRoute("c1")

  expect(await screen.findByText("EXT-UNSCORED")).toBeDefined()
  expect(screen.getByText("未スコア")).toBeDefined()
  await waitFor(() => {
    expect(requested.filter((url) => url.includes("/score-explanation"))).toHaveLength(0)
  })
})

function mockCustomerDetailFetch() {
  vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url = String(input)
    if (url.includes("/scores")) return Promise.resolve(new Response(JSON.stringify([])))
    return Promise.resolve(
      new Response(
        JSON.stringify({
          id: "c1",
          external_id: "EXT-001",
          customer_type: "individual",
          country_code: "JP",
          product_types: [],
          attributes: {},
          created_at: "2025-01-01T00:00:00Z",
          updated_at: "2025-01-01T00:00:00Z",
        }),
      ),
    )
  })
}

test.each([[], null])("keeps the shell for a no-hit response with matches=%s", async (matches) => {
  mockCustomerDetailFetch()
  vi.spyOn(api.customers, "screen").mockResolvedValue({
    customer_id: "c1",
    hit: false,
    matches: matches as never,
    lists_checked: 2,
    screened_at: "2026-08-02T00:00:00Z",
  })

  await renderWithRoute("c1")
  fireEvent.click(await screen.findByRole("button", { name: "スクリーニング" }))

  expect(await screen.findByText("スクリーニング結果")).toBeDefined()
  expect(screen.getByText("ヒットなし")).toBeDefined()
  expect(screen.getByText(/チェック済みリスト: 2件/)).toBeDefined()
  expect(screen.getByText("EXT-001")).toBeDefined()
})
