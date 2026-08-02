import { act, fireEvent, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { api, type BacktestJob, type BacktestResult, type Customer } from "@/lib/api"
import { BacktestPage } from "./backtest"
import { paginatedResponse } from "@/test/api-test-utils"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.useRealTimers()
})

const customer: Customer = {
  id: "c1",
  external_id: "EXT-001",
  customer_type: "individual",
  country_code: "JP",
  product_types: [],
  attributes: {},
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
}

const result: BacktestResult = {
  backtest_id: "bt-1",
  total_transactions: 1,
  total_customers: 1,
  total_alerts: 0,
  scenario_results: [],
  execution_time_ms: 2,
}

function makeJob(status: BacktestJob["status"], overrides: Partial<BacktestJob> = {}): BacktestJob {
  return {
    id: "job-1",
    status,
    from: "2025-01-01T00:00:00Z",
    to: "2025-02-01T00:00:00Z",
    customer_ids: ["c1"],
    scenario_ids: [],
    baseline_rule_set_id: "active",
    candidate_rule_set_id: "candidate",
    progress: status === "completed" ? 1 : 0,
    processed_customers: status === "completed" ? 1 : 0,
    total_customers: 1,
    created_at: "2025-02-01T00:00:00Z",
    updated_at: "2025-02-01T00:00:00Z",
    ...overrides,
  }
}

async function flushInitialLoad() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function startBacktest() {
  fireEvent.click(screen.getByText("EXT-001"))
  fireEvent.change(screen.getByLabelText("候補ルールセット"), { target: { value: "candidate" } })
  fireEvent.click(screen.getByRole("button", { name: "バックテスト実行" }))
  await act(async () => {
    await Promise.resolve()
  })
}

test("renders backtest form with customers", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    paginatedResponse([
        {
          id: "c1",
          external_id: "EXT-001",
          customer_type: "individual",
          country_code: "JP",
          product_types: [],
          attributes: {},
          created_at: "2025-01-01T00:00:00Z",
          updated_at: "2025-01-01T00:00:00Z",
        },
      ]),
  )

  await renderWithRouter(<BacktestPage />)

  expect(await screen.findByText("バックテスト")).toBeDefined()
  expect(await screen.findByText("EXT-001")).toBeDefined()
  expect(screen.getByText("バックテスト実行")).toBeDefined()
})

test("shows error state", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  await renderWithRouter(<BacktestPage />)

  expect(await screen.findByText("データの取得に失敗しました")).toBeDefined()
})

describe("durable backtest polling", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.spyOn(api.customers, "list").mockResolvedValue({
      data: [customer],
      pagination: { has_more: false },
    })
  })

  test("polls with bounded exponential backoff until completion", async () => {
    vi.spyOn(api.backtest, "create").mockResolvedValue(makeJob("queued"))
    const get = vi.spyOn(api.backtest, "get")
      .mockResolvedValueOnce(makeJob("queued"))
      .mockResolvedValueOnce(makeJob("running", { progress: 0.5 }))
      .mockResolvedValueOnce(makeJob("completed", { candidate: result }))

    await renderWithRouter(<BacktestPage />)
    await flushInitialLoad()
    await startBacktest()

    await act(async () => { await vi.advanceTimersByTimeAsync(999) })
    expect(get).toHaveBeenCalledTimes(0)
    await act(async () => { await vi.advanceTimersByTimeAsync(1) })
    expect(get).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(1999) })
    expect(get).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(1) })
    expect(get).toHaveBeenCalledTimes(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(4000) })

    expect(get).toHaveBeenCalledTimes(3)
    expect(screen.getByText("実行結果")).toBeDefined()
    expect(screen.getByRole("status").textContent).toContain("有効な空の結果")
  })

  test("keeps the shell for a completed job with legacy null scenario results", async () => {
    vi.spyOn(api.backtest, "create").mockResolvedValue(makeJob("queued"))
    vi.spyOn(api.backtest, "get").mockResolvedValue(
      makeJob("completed", {
        candidate: { ...result, scenario_results: null as unknown as BacktestResult["scenario_results"] },
      }),
    )

    await renderWithRouter(<BacktestPage />)
    await flushInitialLoad()
    await startBacktest()
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })

    expect(screen.getByText("実行結果")).toBeDefined()
    expect(screen.getByText("EXT-001")).toBeDefined()
  })

  test("shows a polling error and can resume the same job", async () => {
    vi.spyOn(api.backtest, "create").mockResolvedValue(makeJob("queued"))
    const get = vi.spyOn(api.backtest, "get")
      .mockRejectedValueOnce(new Error("database unavailable"))
      .mockResolvedValueOnce(makeJob("completed", { candidate: result }))

    await renderWithRouter(<BacktestPage />)
    await flushInitialLoad()
    await startBacktest()
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })

    expect(screen.getByRole("alert").textContent).toContain("ジョブ状況の取得に失敗しました")
    fireEvent.click(screen.getByRole("button", { name: "ポーリングを再開" }))
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })

    expect(get).toHaveBeenCalledTimes(2)
    expect(screen.getByText("実行結果")).toBeDefined()
  })

  test("stops after ten minutes and offers resume", async () => {
    vi.spyOn(api.backtest, "create").mockResolvedValue(makeJob("queued"))
    vi.spyOn(api.backtest, "get").mockResolvedValue(makeJob("queued"))

    await renderWithRouter(<BacktestPage />)
    await flushInitialLoad()
    await startBacktest()
    await act(async () => { await vi.runAllTimersAsync() })

    expect(screen.getByRole("alert").textContent).toContain("10分以内に完了しませんでした")
    expect(screen.getByRole("button", { name: "ポーリングを再開" })).toBeDefined()
    expect(screen.getByRole("button", { name: "ジョブをキャンセル" })).toBeDefined()
  })

  test("cancels an active job", async () => {
    vi.spyOn(api.backtest, "create").mockResolvedValue(makeJob("queued"))
    vi.spyOn(api.backtest, "get").mockResolvedValue(makeJob("queued"))
    const cancel = vi.spyOn(api.backtest, "cancel").mockResolvedValue(makeJob("cancelled"))

    await renderWithRouter(<BacktestPage />)
    await flushInitialLoad()
    await startBacktest()
    fireEvent.click(screen.getByRole("button", { name: "ジョブをキャンセル" }))
    await act(async () => { await Promise.resolve() })

    expect(cancel).toHaveBeenCalledWith("job-1")
    expect(screen.getByRole("alert").textContent).toContain("キャンセルされました")
  })

  test("aborts the active polling session when unmounted", async () => {
    let createSignal: AbortSignal | undefined
    vi.spyOn(api.backtest, "create").mockImplementation(async (_input, signal) => {
      createSignal = signal
      return makeJob("queued")
    })
    vi.spyOn(api.backtest, "get").mockResolvedValue(makeJob("queued"))

    const view = await renderWithRouter(<BacktestPage />)
    await flushInitialLoad()
    await startBacktest()
    expect(createSignal?.aborted).toBe(false)

    view.unmount()
    expect(createSignal?.aborted).toBe(true)
  })
})
