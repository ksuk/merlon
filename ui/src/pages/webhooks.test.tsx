import { render, screen, fireEvent, waitFor } from "@testing-library/react"
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

test("DLQ tab lists failed deliveries and allows reprocess", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch")
  fetchMock.mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes("/webhooks/dlq/") && url.endsWith("/reprocess")) {
      return Promise.resolve(new Response(JSON.stringify({ success: true, status_code: 200 })))
    }
    if (url.includes("/webhooks/dlq")) {
      return Promise.resolve(
        new Response(
          JSON.stringify([
            {
              id: "dlq1",
              webhook_id: "wh1",
              event_id: "evt-1",
              event: "alert.created",
              payload: "{}",
              attempt_count: 10,
              last_error: "connection refused",
              failed_at: "2025-01-15T10:00:00Z",
            },
          ]),
        ),
      )
    }
    return Promise.resolve(new Response(JSON.stringify([])))
  })

  renderWithRouter(<WebhooksPage />)
  await screen.findByText("Webhookが登録されていません")

  fireEvent.click(screen.getByText("DLQ"))

  await screen.findByText("connection refused")
  expect(screen.getByText("10")).toBeDefined()

  fireEvent.click(screen.getByText("再処理"))

  await waitFor(() => {
    const reprocessCall = fetchMock.mock.calls.find(([url]) =>
      String(url).includes("/webhooks/dlq/dlq1/reprocess"),
    )
    expect(reprocessCall).toBeDefined()
  })
})
