import { screen, fireEvent, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { WebhooksPage } from "./webhooks"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
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

  await renderWithRouter(<WebhooksPage />)

  expect(await screen.findByText("https://example.com/hook")).toBeDefined()
  expect(screen.getByText("アラート作成")).toBeDefined()
  expect(screen.getByText("ケース作成")).toBeDefined()
})

test("shows empty state", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  await renderWithRouter(<WebhooksPage />)

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

  await renderWithRouter(<WebhooksPage />)
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

test("a delete is confirmed in place and names the endpoint it silences", async () => {
  const deleted: string[] = []
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString()
    if (init?.method === "DELETE") {
      deleted.push(url)
      return Promise.resolve(new Response(JSON.stringify({ status: "deleted" })))
    }
    if (url.includes("/system/capabilities")) {
      return Promise.resolve(new Response(JSON.stringify({ auth_mode: "disabled", permissions: [], checked_at: "2026-08-08T00:00:00Z", data: [] })))
    }
    return Promise.resolve(
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
  })

  await renderWithRouter(<WebhooksPage />)

  // The icon-only control must be reachable by its accessible name; before
  // Wave 4 it announced as an empty button.
  const deleteButton = await screen.findByRole("button", { name: "この Webhook を削除" })
  fireEvent.click(deleteButton)

  // The first click must not delete anything.
  expect(deleted).toHaveLength(0)

  const dialog = await screen.findByRole("alertdialog")
  expect(dialog.textContent).toContain("https://example.com/hook")
  expect(dialog.textContent).toContain("2")

  fireEvent.click(screen.getByText("キャンセル"))
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
  expect(deleted).toHaveLength(0)
})
