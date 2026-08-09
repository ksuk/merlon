import { fireEvent, screen, waitFor } from "@testing-library/react"
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

const systemInfo = {
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
}

function statusBody(overrides: Record<string, unknown> = {}) {
  return {
    version: "1.0.0",
    commit: "abcdef123456",
    built_at: "2026-08-08T00:00:00Z",
    auth_mode: "disabled",
    base_currency: "JPY",
    config_digests: { tm_scenarios: "deadbeefcafe" },
    policies: [
      { name: "screening_readiness", policy_version: "2026-08-06-default", digest: "0123456789abcdef", source: "default" },
    ],
    components: [
      { name: "api", configured: true, operational_state: "ready", checked_at: "2026-08-08T00:00:00Z" },
      {
        name: "database",
        configured: true,
        operational_state: "unavailable",
        reason_code: "check_failed",
        checked_at: "2026-08-08T00:00:00Z",
      },
      {
        name: "engine",
        configured: true,
        operational_state: "unknown",
        reason_code: "no_probe_available",
        checked_at: "2026-08-08T00:00:00Z",
      },
    ],
    checked_at: "2026-08-08T00:00:00Z",
    expires_at: "2026-08-08T00:00:15Z",
    source: "live",
    ...overrides,
  }
}

interface SystemStubs {
  info?: unknown
  infoStatus?: number
  status?: unknown
  statusStatus?: number
  onStatusRequest?: (url: string) => void
}

function stubSystem({ info = systemInfo, infoStatus = 200, status, statusStatus = 200, onStatusRequest }: SystemStubs = {}) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/system/status")) {
      onStatusRequest?.(url)
      return Promise.resolve(
        new Response(JSON.stringify(statusStatus === 200 ? (status ?? statusBody()) : { error: "boom" }), {
          status: statusStatus,
        }),
      )
    }
    if (url.includes("/system/info")) {
      return Promise.resolve(
        new Response(JSON.stringify(infoStatus === 200 ? info : { error: "boom" }), { status: infoStatus }),
      )
    }
    return Promise.resolve(new Response(JSON.stringify({})))
  })
}

test("renders system info with features", async () => {
  stubSystem()

  await renderWithRouter(<SystemPage />)

  expect(await screen.findByText("v1.0.0")).toBeDefined()
  expect(screen.getByText("36")).toBeDefined()
  expect(screen.getByText("Go API")).toBeDefined()
  expect(screen.getByText("CDDスコアリング")).toBeDefined()
})

// The defect this page existed to hide: a database refusing connections looked
// exactly like a healthy one, because components were a hardcoded list drawn
// with a green check.
test("does not present a failing component as healthy", async () => {
  stubSystem()

  await renderWithRouter(<SystemPage />)

  await screen.findByText("v1.0.0")
  expect(screen.getByText("利用不可")).toBeDefined()
  expect(screen.getByText("確認を実行し失敗しました。詳細はサーバーログにあります")).toBeDefined()
})

test("reports a component it could not check as unknown, not as ready", async () => {
  stubSystem()

  await renderWithRouter(<SystemPage />)

  await screen.findByText("v1.0.0")
  expect(screen.getByText("不明")).toBeDefined()
  expect(screen.getByText("構成されていますが、ヘルスチェックを公開していません")).toBeDefined()
})

test("shows how old the answer is and can force a live check", async () => {
  const requests: string[] = []
  stubSystem({ onStatusRequest: (url) => requests.push(url) })

  await renderWithRouter(<SystemPage />)

  await screen.findByTestId("status-freshness")
  const before = requests.length

  fireEvent.click(screen.getByRole("button", { name: /再確認/ }))

  await waitFor(() => expect(requests.length).toBeGreaterThan(before))
  expect(requests[requests.length - 1]).toContain("refresh=true")
})

test("shows the active configuration digests and policy provenance", async () => {
  stubSystem()

  await renderWithRouter(<SystemPage />)

  // The API exposed these from Wave 1 and no screen had ever requested them.
  expect(await screen.findByText("deadbeefcafe")).toBeDefined()
  expect(screen.getByText("tm_scenarios")).toBeDefined()
  expect(screen.getByText("組み込み既定値")).toBeDefined()
  expect(screen.getByText("abcdef123456")).toBeDefined()
})

test("keeps the rest of the page when only the readiness lookup fails", async () => {
  stubSystem({ statusStatus: 500 })

  await renderWithRouter(<SystemPage />)

  // Feature configuration is still true even when readiness is unknown.
  expect(await screen.findByText("CDDスコアリング")).toBeDefined()
  expect(screen.getAllByText(/実行時ステータスを読み取れなかった/).length).toBeGreaterThan(0)
})

test("shows error when both lookups fail", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  await renderWithRouter(<SystemPage />)

  expect(await screen.findByText("システム情報の取得に失敗しました")).toBeDefined()
})
