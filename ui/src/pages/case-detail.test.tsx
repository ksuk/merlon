import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
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
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
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
  expect(screen.getByText("不審な取引パターン")).toBeDefined()
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
  expect(screen.getByText("STR対象")).toBeDefined()
})

test("hides note form for closed case", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
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
