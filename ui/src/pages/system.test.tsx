import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { SystemPage } from "./system"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders system info with features", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        version: "1.0.0",
        components: ["api", "engine", "database"],
        endpoints: 36,
        features: {
          auth: true,
          audit: true,
          cases: true,
          webhooks: true,
          rate_limit: false,
          scoring: true,
          monitoring: true,
          screening: true,
          backtest: true,
          config: true,
        },
      }),
    ),
  )

  await renderWithRouter(<SystemPage />)

  expect(await screen.findByText("v1.0.0")).toBeDefined()
  expect(screen.getByText("36")).toBeDefined()
  expect(screen.getByText("Go API")).toBeDefined()
  expect(screen.getByText("Go Engine")).toBeDefined()
  expect(screen.getByText("CDDスコアリング")).toBeDefined()
})

test("shows error on fetch failure", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  await renderWithRouter(<SystemPage />)

  expect(await screen.findByText("システム情報の取得に失敗しました")).toBeDefined()
})
