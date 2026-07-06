import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { ConfigPage } from "./config"

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("renders config validation form", async () => {
  await renderWithRouter(<ConfigPage />)

  expect(screen.getByText("設定検証")).toBeDefined()
  expect(screen.getByText("CDD重み付け")).toBeDefined()
  expect(screen.getByText("シナリオルール")).toBeDefined()
  expect(screen.getByText("検証")).toBeDefined()
})

test("renders config type buttons", async () => {
  await renderWithRouter(<ConfigPage />)

  expect(screen.getByText("スクリーニングリスト")).toBeDefined()
})
