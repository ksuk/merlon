import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { APIKeysPage } from "./apikeys"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders API key list", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          id: "k1",
          name: "本番用",
          role: "admin",
          active: true,
          created_at: "2025-01-15T10:00:00Z",
        },
      ]),
    ),
  )

  await renderWithRouter(<APIKeysPage />)

  expect(await screen.findByText("本番用")).toBeDefined()
  expect(screen.getByText("管理者")).toBeDefined()
})

test("shows empty state", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([])))

  await renderWithRouter(<APIKeysPage />)

  expect(await screen.findByText("APIキーが登録されていません")).toBeDefined()
})
