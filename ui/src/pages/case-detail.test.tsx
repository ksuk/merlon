import { fireEvent, screen, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { paginatedResponse } from "@/test/api-test-utils"
import { CaseDetailPage } from "./case-detail"

function renderWithRoute(id: string) {
  return renderWithI18n(
    <MemoryRouter initialEntries={[`/cases/${id}`]}>
      <Routes>
        <Route path="cases/:id" element={<CaseDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders case detail with notes and transitions", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
    new Response(
      JSON.stringify({
        id: "case1",
        customer_id: "c1",
        alert_ids: ["a1"],
        status: "investigating",
        priority: "high",
        assigned_to: "tanaka",
        summary: "不審な取引パターン",
        notes: [
          {
            id: "n1",
            author: "tanaka",
            content: "調査を開始しました",
            created_at: "2025-01-15T10:00:00Z",
          },
        ],
        created_at: "2025-01-14T09:00:00Z",
        updated_at: "2025-01-15T10:00:00Z",
      }),
    ),
  )

  await renderWithRoute("case1")

  expect(await screen.findByText("ケース詳細")).toBeDefined()
  await waitFor(() => expect(screen.getByRole("textbox", { name: "概要" })).toHaveValue("不審な取引パターン"))
  expect(screen.getByText("不審な取引パターン", { selector: "p" })).toBeDefined()
  expect(screen.getByText("調査を開始しました")).toBeDefined()
  expect(screen.getAllByText("tanaka").length).toBeGreaterThan(0)
  expect(screen.getByText("エスカレーション")).toBeDefined()
  expect(screen.getByText("クローズ")).toBeDefined()
})

test("shows related cases section", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch")
  fetchMock.mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes("/related")) {
      return Promise.resolve(
        new Response(
          JSON.stringify([
            {
              case: {
                id: "case-old",
                customer_id: "c1",
                alert_ids: [],
                status: "str_filed",
                priority: "high",
                summary: "過去のSTR対象ケース",
                created_at: "2024-06-01T09:00:00Z",
                updated_at: "2024-06-02T09:00:00Z",
              },
              link_type: "auto",
            },
          ]),
        ),
      )
    }
    if (url.includes("/customers/")) {
      return Promise.resolve(new Response(JSON.stringify({ id: "c1", external_id: "EXT1" })))
    }
    return Promise.resolve(
      new Response(
        JSON.stringify({
          id: "case1",
          customer_id: "c1",
          alert_ids: [],
          status: "investigating",
          priority: "high",
          summary: "不審な取引パターン",
          notes: [],
          created_at: "2025-01-15T10:00:00Z",
          updated_at: "2025-01-15T10:00:00Z",
        }),
      ),
    )
  })

  await renderWithRoute("case1")

  expect(await screen.findByText("関連ケース")).toBeDefined()
  expect(await screen.findByText("case-old")).toBeDefined()
  expect(screen.getByText("自動抽出（同一顧客）")).toBeDefined()
  expect(screen.getByText("STR届出済み")).toBeDefined()
})

test("hides note form for closed case", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
    new Response(
      JSON.stringify({
        id: "case2",
        customer_id: "c1",
        alert_ids: [],
        status: "closed",
        priority: "low",
        summary: "完了済み",
        notes: [],
        created_at: "2025-01-14T09:00:00Z",
        updated_at: "2025-01-16T10:00:00Z",
        closed_at: "2025-01-16T10:00:00Z",
      }),
    ),
  )

  await renderWithRoute("case2")

  expect(await screen.findByText("ケース詳細")).toBeDefined()
  expect(screen.queryByPlaceholderText("ノートを追加...")).toBeNull()
})

test("shows a reload path when a case update conflicts", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (init?.method === "PATCH") {
      return new Response(JSON.stringify({ error: "conflict", error_code: "conflict" }), { status: 409 })
    }
    if (url.includes("/related")) return new Response(JSON.stringify([]))
    if (url.includes("/customers/")) return new Response(JSON.stringify({ id: "c1", external_id: "EXT1" }))
    return new Response(JSON.stringify({
      id: "case3",
      customer_id: "c1",
      alert_ids: [],
      status: "investigating",
      priority: "high",
      summary: "競合テストケース",
      notes: [],
      created_at: "2025-01-15T10:00:00Z",
      updated_at: "2025-01-15T10:00:00Z",
    }))
  })

  await renderWithRoute("case3")
  await screen.findByText("競合テストケース")
  fireEvent.click(screen.getByText("クローズ"))
  fireEvent.change(screen.getByLabelText("クローズ理由"), { target: { value: "reviewed" } })
  fireEvent.click(screen.getByRole("button", { name: "確定" }))

  expect((await screen.findAllByRole("alert"))[0]).toHaveTextContent("別のセッションでケースが更新されました")
  expect(screen.getByRole("button", { name: "現在のケースを再読み込み" })).toBeDefined()
  const patchCall = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH")
  expect(JSON.parse((patchCall?.[1] as RequestInit).body as string).expected_updated_at).toBe("2025-01-15T10:00:00Z")
})

test("requires a case closure rationale and allows cancel without mutating", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(JSON.stringify({
      id: "case-close",
      customer_id: "c1",
      alert_ids: [],
      status: "investigating",
      priority: "high",
      summary: "クローズ確認ケース",
      notes: [],
      created_at: "2025-01-15T10:00:00Z",
      updated_at: "2025-01-15T10:00:00Z",
    })))

  await renderWithRoute("case-close")
  await screen.findByText("クローズ確認ケース")
  fireEvent.click(screen.getByRole("button", { name: "クローズ" }))
  fireEvent.click(screen.getByRole("button", { name: "確定" }))
  expect((await screen.findAllByRole("alert"))[0]).toHaveTextContent("クローズ理由を入力してください")

  fireEvent.change(screen.getByLabelText("クローズ理由"), { target: { value: "調査完了" } })
  fireEvent.click(screen.getByRole("button", { name: "キャンセル" }))
  expect(screen.queryByLabelText("クローズ理由")).toBeNull()
  expect(fetchMock.mock.calls.some(([, request]) => request?.method === "PATCH")).toBe(false)
})

test("renders the mixed investigation case file and exposes stable references", async () => {
  const caseData = {
    id: "case-file-ui",
    customer_id: "c1",
    alert_ids: ["alert-1"],
    status: "investigating",
    priority: "high",
    summary: "調査ファイル表示ケース",
    notes: [],
    created_at: "2026-08-01T09:00:00Z",
    updated_at: "2026-08-01T09:30:00Z",
  }
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/timeline")) {
      return new Response(JSON.stringify({
        case: caseData,
        events: [{ id: "event-ui-1", case_id: caseData.id, event_type: "status_changed", actor: "analyst", reason: "reviewed", related_alert_ids: ["alert-1"], created_at: "2026-08-01T10:00:00Z" }],
        evidence: [{ id: "evidence-ui-1", case_id: caseData.id, description: "銀行明細", source: "bank-api", evidence_type: "statement", collected_at: "2026-08-01T10:01:00Z", collected_by: "analyst", version: 1, created_at: "2026-08-01T10:01:00Z" }],
        checklist: [{ id: "check-ui-1", case_id: caseData.id, key: "cdd", label: "CDD確認済み", completed: true, version: 1, created_at: "2026-08-01T10:02:00Z", updated_at: "2026-08-01T10:02:00Z" }],
        work_items: [{ id: "work-ui-1", case_id: caseData.id, title: "届出番号を確認", status: "open", created_at: "2026-08-01T10:03:00Z", updated_at: "2026-08-01T10:03:00Z" }],
        relationships: [],
      }), { headers: { "Content-Type": "application/json" } })
    }
    if (url.includes("/related")) return new Response(JSON.stringify([]))
    if (url.includes("/customers/")) return new Response(JSON.stringify({ id: "c1", external_id: "EXT1" }))
    if (url.includes("/operators/directory")) return new Response(JSON.stringify({ users: [], teams: [] }))
    if (url.endsWith("/export")) return new Response("{}")
    return new Response(JSON.stringify(caseData), { headers: { "Content-Type": "application/json" } })
  })
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:case-file")
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)

  await renderWithRoute("case-file-ui")
  expect(await screen.findByText("調査ファイル表示ケース")).toBeDefined()
  expect(await screen.findByText("調査ファイル")).toBeDefined()
  expect(screen.getByText("status_changed")).toBeDefined()
  expect(screen.getByText("銀行明細")).toBeDefined()
  expect(screen.getByText("CDD確認済み")).toBeDefined()
  expect(screen.getByText("届出番号を確認")).toBeDefined()

  fireEvent.click(screen.getByRole("button", { name: "ケースファイルを出力" }))
  await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/cases/case-file-ui/export"))).toBe(true))
})

test("requires a correction reason and appends corrected evidence", async () => {
  const caseData = {
    id: "case-evidence-correction",
    customer_id: "c1",
    alert_ids: [],
    status: "investigating",
    priority: "medium",
    summary: "証跡訂正ケース",
    notes: [],
    created_at: "2026-08-01T09:00:00Z",
    updated_at: "2026-08-01T09:30:00Z",
  }
  const evidence = { id: "evidence-old", case_id: caseData.id, description: "旧明細", source: "bank-api", evidence_type: "statement", collected_by: "analyst", version: 1, created_at: "2026-08-01T10:00:00Z" }
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (init?.method === "POST" && url.includes("/corrections")) return new Response(JSON.stringify({ ...evidence, id: "evidence-new", description: "訂正明細", version: 2 }))
    if (url.endsWith("/timeline")) return new Response(JSON.stringify({ case: caseData, events: [], evidence: [evidence], checklist: [], work_items: [], relationships: [] }))
    if (url.endsWith("/related")) return new Response(JSON.stringify([]))
    if (url.includes("/customers/")) return new Response(JSON.stringify({ id: "c1", external_id: "EXT1" }))
    if (url.includes("/operators/directory")) return new Response(JSON.stringify({ users: [], teams: [] }))
    return new Response(JSON.stringify(caseData))
  })

  await renderWithRoute(caseData.id)
  await screen.findByText("旧明細")
  fireEvent.click(screen.getByRole("button", { name: "証跡を訂正" }))
  fireEvent.click(screen.getAllByRole("button", { name: "証跡を訂正" })[1])
  expect(await screen.findByRole("alert")).toHaveTextContent("訂正理由を入力してください")
  expect(fetchMock.mock.calls.some(([input, request]) => String(input).includes("/corrections") && request?.method === "POST")).toBe(false)

  fireEvent.change(screen.getByPlaceholderText("訂正理由"), { target: { value: "原本の再確認" } })
  fireEvent.change(screen.getByPlaceholderText("説明"), { target: { value: "訂正明細" } })
  fireEvent.click(screen.getAllByRole("button", { name: "証跡を訂正" })[1])
  await waitFor(() => expect(fetchMock.mock.calls.some(([input, request]) => String(input).includes("/evidence/evidence-old/corrections") && request?.method === "POST")).toBe(true))
  const correctionCall = fetchMock.mock.calls.find(([input, request]) => String(input).includes("/evidence/evidence-old/corrections") && request?.method === "POST")
  expect(JSON.parse((correctionCall?.[1] as RequestInit).body as string)).toMatchObject({ reason: "原本の再確認", description: "訂正明細" })
})

test("shows a case-file export error without hiding the investigation file", async () => {
  const caseData = {
    id: "case-export-error",
    customer_id: "c1",
    alert_ids: [],
    status: "investigating",
    priority: "medium",
    summary: "出力失敗ケース",
    notes: [],
    created_at: "2026-08-01T09:00:00Z",
    updated_at: "2026-08-01T09:30:00Z",
  }
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/export")) return new Response(JSON.stringify({ error: "export unavailable", error_code: "service_unavailable" }), { status: 503 })
    if (url.endsWith("/timeline")) return new Response(JSON.stringify({ case: caseData, events: [], evidence: [], checklist: [], work_items: [], relationships: [] }))
    if (url.includes("/related")) return new Response(JSON.stringify([]))
    if (url.includes("/customers/")) return new Response(JSON.stringify({ id: "c1", external_id: "EXT1" }))
    if (url.includes("/operators/directory")) return new Response(JSON.stringify({ users: [], teams: [] }))
    return new Response(JSON.stringify(caseData))
  })

  await renderWithRoute("case-export-error")
  await screen.findByText("出力失敗ケース")
  fireEvent.click(screen.getByRole("button", { name: "ケースファイルを出力" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("この機能は現在利用できません")
  expect(screen.getByText("調査ファイル")).toBeDefined()
})

test("searches accessible related-case results and preserves rationale after an add error", async () => {
  const caseData = {
    id: "case-related-ui",
    customer_id: "c1",
    alert_ids: [],
    status: "investigating",
    priority: "medium",
    summary: "関連付け元ケース",
    notes: [],
    created_at: "2026-08-01T09:00:00Z",
    updated_at: "2026-08-01T09:30:00Z",
  }
  const candidate = { ...caseData, id: "case-target-ui", summary: "リンク候補ケース" }
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (init?.method === "POST" && url.endsWith("/related")) return new Response(JSON.stringify({ error: "link conflict", error_code: "conflict" }), { status: 409 })
    if (url.endsWith("/related")) return new Response(JSON.stringify([]))
    if (url.includes("/timeline")) return new Response(JSON.stringify({ case: caseData, events: [], evidence: [], checklist: [], work_items: [], relationships: [] }))
    if (url.includes("/cases?")) return paginatedResponse([candidate])
    if (url.includes("/customers/")) return new Response(JSON.stringify({ id: "c1", external_id: "EXT1" }))
    if (url.includes("/operators/directory")) return new Response(JSON.stringify({ users: [], teams: [] }))
    return new Response(JSON.stringify(caseData))
  })

  await renderWithRoute("case-related-ui")
  await screen.findByText("関連付け元ケース")
  fireEvent.click(screen.getByRole("button", { name: "関連ケースを追加" }))
  fireEvent.change(screen.getByLabelText("関連ケースを検索"), { target: { value: "target" } })
  fireEvent.click(screen.getByRole("button", { name: "検索" }))
  expect(await screen.findByRole("button", { name: /case-target-ui/ })).toBeDefined()
  fireEvent.click(screen.getByRole("button", { name: /case-target-ui/ }))
  fireEvent.change(screen.getByLabelText("リンク理由"), { target: { value: "同一顧客の関連調査" } })
  fireEvent.click(screen.getByRole("button", { name: "リンクを追加" }))

  expect(await screen.findByRole("alert")).toHaveTextContent("この操作はリソースの現在の状態と競合しています")
  expect(screen.getByDisplayValue("case-target-ui")).toBeDefined()
  expect(screen.getByDisplayValue("同一顧客の関連調査")).toBeDefined()
  expect(fetchMock.mock.calls.some(([, request]) => request?.method === "POST")).toBe(true)

  fireEvent.click(screen.getByRole("button", { name: "キャンセル" }))
  expect(screen.queryByLabelText("関連ケースを検索")).toBeNull()
})

test("corrects and removes a related case with explicit history reasons", async () => {
  const caseData = {
    id: "case-related-actions",
    customer_id: "c1",
    alert_ids: [],
    status: "investigating",
    priority: "medium",
    summary: "関連操作ケース",
    notes: [],
    created_at: "2026-08-01T09:00:00Z",
    updated_at: "2026-08-01T09:30:00Z",
  }
  const makeRelated = (id: string, type: string) => ({
    case: { ...caseData, id, summary: id },
    link_type: "manual" as const,
    relationship: { id: `relationship-${id}`, case_id: caseData.id, related_case_id: id, relationship_type: type, rationale: "initial rationale", created_by: "analyst", created_at: "2026-08-01T10:00:00Z", active: true, source: "manual" as const },
  })
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.endsWith("/related/relationship-case-related-1")) return new Response(JSON.stringify({ id: "relationship-case-related-1", relationship_type: "same_customer", rationale: "corrected" }))
    if (url.endsWith("/related/relationship-case-related-2")) return new Response(JSON.stringify({ relationship_id: "relationship-case-related-2", active: false }))
    if (url.endsWith("/related")) return new Response(JSON.stringify([makeRelated("case-related-1", "related"), makeRelated("case-related-2", "same_customer")]))
    if (url.includes("/timeline")) return new Response(JSON.stringify({ case: caseData, events: [], evidence: [], checklist: [], work_items: [], relationships: [] }))
    if (url.includes("/customers/")) return new Response(JSON.stringify({ id: "c1", external_id: "EXT1" }))
    if (url.includes("/operators/directory")) return new Response(JSON.stringify({ users: [], teams: [] }))
    return new Response(JSON.stringify(caseData))
  })
  await renderWithRoute("case-related-actions")
  await screen.findByText("case-related-1")
  fireEvent.click(screen.getAllByRole("button", { name: "リンクを訂正" })[0])
  fireEvent.change(screen.getByPlaceholderText("訂正理由"), { target: { value: "relationship type corrected" } })
  fireEvent.click(screen.getByRole("button", { name: "訂正を保存" }))
  await waitFor(() => expect(fetchMock.mock.calls.some(([, request]) => request?.method === "PUT")).toBe(true))
  const putCall = fetchMock.mock.calls.find(([, request]) => request?.method === "PUT")
  expect(JSON.parse((putCall?.[1] as RequestInit).body as string)).toMatchObject({ rationale: "relationship type corrected" })

  fireEvent.click(screen.getAllByRole("button", { name: "リンクを削除" })[1])
  fireEvent.change(screen.getByRole("dialog").querySelector("input")!, { target: { value: "obsolete link" } })
  fireEvent.click(screen.getByRole("button", { name: "関連付けを削除" }))
  await waitFor(() => expect(fetchMock.mock.calls.some(([, request]) => request?.method === "DELETE")).toBe(true))
  const deleteCall = fetchMock.mock.calls.find(([, request]) => request?.method === "DELETE")
  expect(JSON.parse((deleteCall?.[1] as RequestInit).body as string).reason).toBe("obsolete link")
})

test("files a case only with a submitted report and keeps filing input after a conflict", async () => {
  const caseData = {
    id: "case-filing-ui",
    customer_id: "c1",
    alert_ids: ["alert-filing"],
    status: "investigating",
    priority: "high",
    summary: "STR届出ケース",
    notes: [],
    created_at: "2026-08-01T09:00:00Z",
    updated_at: "2026-08-01T09:30:00Z",
  }
  const submittedReport = {
    id: "report-submitted-ui",
    alert_id: "alert-filing",
    case_id: caseData.id,
    customer_id: "c1",
    report_type: "str",
    status: "submitted",
    suspicious_point: "submitted suspicious point",
    transaction_ids: [],
    transaction_snapshot: [],
    total_amount: 0,
    currency: "JPY",
    created_at: "2026-08-01T09:10:00Z",
    updated_at: "2026-08-01T09:20:00Z",
    created_by: "analyst",
    submitted_at: "2026-08-01T09:20:00Z",
    submitted_by: "analyst",
    submission_evidence: "receipt-1",
  }
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (init?.method === "PATCH") return new Response(JSON.stringify({ error: "filing conflict", error_code: "conflict" }), { status: 409 })
    if (url.includes("/reports/str")) return paginatedResponse([submittedReport])
    if (url.endsWith("/related")) return new Response(JSON.stringify([]))
    if (url.endsWith("/timeline")) return new Response(JSON.stringify({ case: caseData, events: [], evidence: [], checklist: [], work_items: [], relationships: [] }))
    if (url.includes("/customers/")) return new Response(JSON.stringify({ id: "c1", external_id: "EXT1" }))
    if (url.includes("/operators/directory")) return new Response(JSON.stringify({ users: [], teams: [] }))
    return new Response(JSON.stringify(caseData))
  })

  await renderWithRoute("case-filing-ui")
  await screen.findByText("STR届出ケース")
  fireEvent.click(screen.getByRole("button", { name: "STR対象として届出" }))
  expect(await screen.findByText("STR届出")).toBeDefined()
  fireEvent.change(screen.getByPlaceholderText("レポートIDを選択または入力"), { target: { value: submittedReport.id } })
  fireEvent.change(screen.getByPlaceholderText("届出経路"), { target: { value: "secure-portal" } })
  fireEvent.change(screen.getByPlaceholderText("届出先"), { target: { value: "JAFIC" } })
  fireEvent.change(screen.getByPlaceholderText("外部届出番号"), { target: { value: "receipt-1" } })
  fireEvent.change(screen.getByLabelText("根拠"), { target: { value: "届出内容を確認済み" } })
  fireEvent.click(screen.getByRole("button", { name: "届出を確定" }))

  expect(await screen.findByRole("alert")).toHaveTextContent("別のセッションでケースが更新されました")
  expect(screen.getByDisplayValue(submittedReport.id)).toBeDefined()
  expect(screen.getByDisplayValue("secure-portal")).toBeDefined()
  const patchCall = fetchMock.mock.calls.find(([, request]) => request?.method === "PATCH")
  expect(JSON.parse((patchCall?.[1] as RequestInit).body as string)).toMatchObject({
    status: "str_filed",
    str_report_id: submittedReport.id,
    confirm: true,
    external_reference: "receipt-1",
  })
})
