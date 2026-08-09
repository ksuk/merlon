import { fireEvent, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { AlertDetailPage } from "./alert-detail"

function renderWithRoute(id: string) {
  return renderWithI18n(
    <MemoryRouter initialEntries={[`/alerts/${id}`]}>
      <Routes>
        <Route path="alerts/:id" element={<AlertDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

const baseAlert = {
  id: "a1",
  customer_id: "c1",
  scenario_id: "tm_structuring_basic",
  severity: "high",
  status: "open",
  score: 88.5,
  description: "大口取引の検出",
  transaction_ids: ["t1"],
  detected_at: "2025-01-15T10:00:00Z",
  created_at: "2025-01-15T10:00:00Z",
  updated_at: "2025-01-15T10:00:00Z",
}

function mockAlertDetail(overrides: Record<string, unknown>) {
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/alerts/a1") && !url.includes("/decisions")) {
      return Promise.resolve(new Response(JSON.stringify({ ...baseAlert, ...overrides })))
    }
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

function renderAlertDetail() {
  return renderWithRoute("a1")
}

test("renders alert detail with status transitions", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        id: "a1",
        customer_id: "c1",
        scenario_id: "large_tx",
        severity: "high",
        status: "open",
        score: 88.5,
        description: "大口取引の検出",
        transaction_ids: ["t1", "t2"],
        detected_at: "2025-01-15T10:00:00Z",
        created_at: "2025-01-15T10:00:00Z",
        updated_at: "2025-01-15T10:00:00Z",
      }),
    ),
  )

  await renderWithRoute("a1")

  expect(await screen.findByText("アラート詳細")).toBeDefined()
  expect(screen.getByText("大口取引の検出")).toBeDefined()
  expect(screen.getByText("調査開始")).toBeDefined()
  expect(screen.getByText("エスカレーション")).toBeDefined()
})

test("offers an explicit reopen transition for a closed alert", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        id: "a2",
        customer_id: "c1",
        scenario_id: "large_tx",
        severity: "low",
        status: "closed_false_positive",
        score: 30.0,
        description: "偽陽性",
        transaction_ids: [],
        detected_at: "2025-01-15T10:00:00Z",
        resolved_at: "2025-01-16T10:00:00Z",
        resolved_by: "operator",
        created_at: "2025-01-15T10:00:00Z",
        updated_at: "2025-01-16T10:00:00Z",
      }),
    ),
  )

  await renderWithRoute("a2")

  expect(await screen.findByText("アラート詳細")).toBeDefined()
  expect(screen.getByText("ステータス変更")).toBeDefined()
  expect(screen.getByText("調査を再開")).toBeDefined()
})

test("shows a reload path when a status update conflicts", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) => {
    if (init?.method === "PATCH") {
      return new Response(JSON.stringify({ error: "conflict", error_code: "conflict" }), { status: 409 })
    }
    return new Response(JSON.stringify({
      id: "a3",
      customer_id: "c1",
      scenario_id: "large_tx",
      severity: "high",
      status: "open",
      score: 88.5,
      description: "競合テスト",
      transaction_ids: [],
      detected_at: "2025-01-15T10:00:00Z",
      created_at: "2025-01-15T10:00:00Z",
      updated_at: "2025-01-15T10:00:00Z",
    }))
  })

  await renderWithRoute("a3")
  await screen.findByText("競合テスト")
  fireEvent.click(screen.getByText("調査開始"))

  expect(await screen.findByRole("alert")).toHaveTextContent("別のセッションでアラートが更新されました")
  expect(screen.getByRole("button", { name: "現在のアラートを再読み込み" })).toBeDefined()
  const patchCall = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH")
  expect(JSON.parse((patchCall?.[1] as RequestInit).body as string).expected_updated_at).toBe("2025-01-15T10:00:00Z")
})

test("requires rationale and confirmation, then retains it when the decision fails", async () => {
  const alert = {
    id: "a-decision",
    customer_id: "c1",
    scenario_id: "large_tx",
    severity: "high",
    status: "investigating",
    score: 88.5,
    description: "判定確認テスト",
    transaction_ids: [],
    detected_at: "2025-01-15T10:00:00Z",
    created_at: "2025-01-15T10:00:00Z",
    updated_at: "2025-01-15T10:00:00Z",
  }
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) => {
    if (init?.method === "PATCH") {
      return new Response(JSON.stringify({ error: "decision conflict", error_code: "conflict" }), { status: 409 })
    }
    return new Response(JSON.stringify(alert), { headers: { "Content-Type": "application/json" } })
  })

  await renderWithRoute("a-decision")
  await screen.findByText("判定確認テスト")
  fireEvent.click(screen.getByRole("button", { name: "真陽性として完了" }))
  expect(await screen.findByRole("dialog")).toBeDefined()

  fireEvent.click(screen.getByRole("button", { name: "判定を確定" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("判定理由を入力してください")
  const rationale = screen.getByRole("textbox", { name: "判定理由" })
  fireEvent.change(rationale, { target: { value: "追加証拠を確認したため" } })
  fireEvent.click(screen.getByRole("button", { name: "判定を確定" }))

  expect(await screen.findByRole("alert")).toHaveTextContent("別のセッションでアラートが更新されました")
  expect(rationale).toHaveValue("追加証拠を確認したため")
  const patchCall = fetchMock.mock.calls.find(([, request]) => request?.method === "PATCH")
  expect(JSON.parse((patchCall?.[1] as RequestInit).body as string)).toMatchObject({
    rationale: "追加証拠を確認したため",
    confirm: true,
  })
})

test("canceling an alert decision does not send a mutation", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({
      id: "a-cancel",
      customer_id: "c1",
      scenario_id: "large_tx",
      severity: "medium",
      status: "investigating",
      score: 60,
      description: "キャンセルテスト",
      transaction_ids: [],
      detected_at: "2025-01-15T10:00:00Z",
      created_at: "2025-01-15T10:00:00Z",
      updated_at: "2025-01-15T10:00:00Z",
    })),
  )

  await renderWithRoute("a-cancel")
  await screen.findByText("キャンセルテスト")
  fireEvent.click(screen.getByRole("button", { name: "真陽性として完了" }))
  fireEvent.click(await screen.findByRole("button", { name: "キャンセル" }))

  expect(screen.queryByRole("dialog")).toBeNull()
  expect(fetchMock.mock.calls.some(([, request]) => request?.method === "PATCH")).toBe(false)
})

test("shows an alert with no provenance as not captured, without inventing one", async () => {
  mockAlertDetail({
    provenance: { scenario_id: "tm_structuring_basic", availability: "not_captured" },
  })

  await renderAlertDetail()

  expect(await screen.findByText("未記録")).toBeDefined()
  expect(screen.getByText(/現在の設定は意図的に表示しません/)).toBeDefined()
})

test("links a resolved rule version to the rule it names", async () => {
  mockAlertDetail({
    provenance: {
      scenario_id: "tm_structuring_basic",
      availability: "restricted",
      rule_name: "tm_structuring_basic",
      rule_version: 3,
      rule_digest: "0123456789abcdef0123",
      evaluation_mode: "batch",
      applied_threshold: 1000000,
      config_digests: { tm_scenarios: "digest-abc" },
    },
  })

  await renderAlertDetail()

  const link = await screen.findByRole("link", { name: "tm_structuring_basic @ v3" })
  expect(link.getAttribute("href")).toBe("/rules/tm_structuring_basic")
  expect(screen.getByText("digest-abc")).toBeDefined()
  expect(screen.getByText("1000000")).toBeDefined()
})

test("keeps the captured facts when the rule reference cannot be resolved", async () => {
  mockAlertDetail({
    provenance: {
      scenario_id: "tm_scenario_since_deleted",
      availability: "missing",
      config_digests: { tm_scenarios: "digest-old" },
    },
  })

  await renderAlertDetail()

  expect(await screen.findByText("参照先を解決できません")).toBeDefined()
  expect(screen.getByText("digest-old")).toBeDefined()
  expect(screen.queryByRole("link", { name: /@ v/ })).toBeNull()
})
