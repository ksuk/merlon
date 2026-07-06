import { screen } from "@testing-library/react"
import { expect, test } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { NotFoundPage } from "./not-found"

test("renders 404 page", async () => {
  await renderWithI18n(
    <MemoryRouter>
      <NotFoundPage />
    </MemoryRouter>,
  )

  expect(screen.getByText("404")).toBeDefined()
  expect(screen.getByText("ページが見つかりません")).toBeDefined()
  expect(screen.getByText("ダッシュボードに戻る")).toBeDefined()
})
