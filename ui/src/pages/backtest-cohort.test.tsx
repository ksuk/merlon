import { fireEvent, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { BacktestPage } from "./backtest"

// #71 requires the cohort to be previewed before execution and an empty one to
// warn or block. The only preview was computed inside job creation, so the
// warning arrived after the comparison had already started, and the
// comparison column labels were English literals in an otherwise localized
// screen.

const customers = {
  data: [
    {
      id: "cust-1",
      external_id: "EXT-1",
      customer_type: "individual",
      country_code: "JP",
      product_types: [],
      attributes: {},
      status: "active",
      created_at: "2025-01-01T00:00:00Z",
      updated_at: "2025-01-01T00:00:00Z",
    },
  ],
  pagination: { has_more: false },
}

const emptyCohort = {
  customer_count: 1,
  transaction_count: 0,
  transaction_counted: true,
  sample_customer_ids: ["cust-1"],
  empty: true,
  warnings: ["the selected customers have no transactions; the comparison would produce an empty result"],
}

const requestedURLs: string[] = []

function mockAPI() {
  requestedURLs.length = 0
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    requestedURLs.push(url)
    if (url.includes("/backtests/preview")) {
      return Promise.resolve(new Response(JSON.stringify(emptyCohort)))
    }
    if (url.includes("/customers")) {
      return Promise.resolve(new Response(JSON.stringify(customers)))
    }
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("an empty cohort is warned about before any job is created", async () => {
  mockAPI()
  await renderWithI18n(
    <MemoryRouter>
      <BacktestPage />
    </MemoryRouter>,
  )

  fireEvent.click(await screen.findByRole("button", { name: /EXT-1/ }))
  fireEvent.click(screen.getByTestId("backtest-preview-cohort"))

  const panel = await screen.findByTestId("backtest-cohort-preview")
  expect(panel.textContent).toContain("取引: 0件")
  expect(panel.textContent).toContain("比較対象がありません")

  // The warning must not have required a job: nothing was posted to /backtests.
  expect(requestedURLs.some((url) => url.endsWith("/backtests"))).toBe(false)
  expect(requestedURLs.some((url) => url.includes("/backtests/preview"))).toBe(true)
})
