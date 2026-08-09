import { screen } from "@testing-library/react"
import { beforeEach, expect, test, vi } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { paginatedResponse } from "@/test/api-test-utils"
import { ScreeningQueuePage } from "./screening-queue"

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders a durable screening hit with an accessible rationale control", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    paginatedResponse([{
      id: "screen-1",
      customer_id: "cust-1",
      list_id: "mof-japan",
      list_type: "sanctions",
      entry_id: "entry-1",
      matched_name: "Test Match",
      similarity: 0.98,
      status: "NEW",
      screened_at: "2026-08-05T00:00:00Z",
      created_at: "2026-08-05T00:00:00Z",
      suppressed: false,
      version: 1,
      updated_at: "2026-08-05T00:00:00Z",
    }]),
  )

  renderWithI18n(<MemoryRouter><ScreeningQueuePage /></MemoryRouter>)

  expect(await screen.findByText("Test Match")).toBeDefined()
  expect(screen.getByRole("textbox", { name: "判定理由" })).toBeDefined()
  expect(screen.getAllByText("未確認").length).toBeGreaterThan(0)
})
