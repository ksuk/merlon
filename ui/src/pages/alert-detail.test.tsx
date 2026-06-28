import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { AlertDetailPage } from "./alert-detail"

function renderWithRoute(id: string) {
  return render(
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

  renderWithRoute("a1")

  expect(await screen.findByText("アラート詳細")).toBeDefined()
  expect(screen.getByText("大口取引の検出")).toBeDefined()
  expect(screen.getByText("調査開始")).toBeDefined()
  expect(screen.getByText("エスカレーション")).toBeDefined()
})

test("hides transitions for closed alert", async () => {
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

  renderWithRoute("a2")

  expect(await screen.findByText("アラート詳細")).toBeDefined()
  expect(screen.queryByText("ステータス変更")).toBeNull()
})
