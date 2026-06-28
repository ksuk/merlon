import { render, screen } from "@testing-library/react"
import { expect, test } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { NotFoundPage } from "./not-found"

test("renders 404 page", () => {
  render(
    <MemoryRouter>
      <NotFoundPage />
    </MemoryRouter>,
  )

  expect(screen.getByText("404")).toBeDefined()
  expect(screen.getByText("ページが見つかりません")).toBeDefined()
  expect(screen.getByText("ダッシュボードに戻る")).toBeDefined()
})
