import { render, screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { WebhooksPage } from "./webhooks"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders webhook list", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          id: "wh1",
          url: "https://example.com/hook",
          events: ["alert.created", "case.created"],
          active: true,
          created_at: "2025-01-15T10:00:00Z",
          updated_at: "2025-01-15T10:00:00Z",
        },
      ]),
    ),
  )

  renderWithRouter(<WebhooksPage />)

  expect(await screen.findByText("https://example.com/hook")).toBeDefined()
  expect(screen.getByText("アラート作成")).toBeDefined()
  expect(screen.getByText("ケース作成")).toBeDefined()
})

test("shows empty state", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  renderWithRouter(<WebhooksPage />)

  expect(await screen.findByText("Webhookが登録されていません")).toBeDefined()
})
