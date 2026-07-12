import { screen } from "@testing-library/react"
import { expect, test, vi } from "vitest"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { ErrorBoundary } from "./error-boundary"

function ThrowError(): React.ReactNode {
  throw new Error("テストエラー")
}

test("renders error message when child throws", async () => {
  vi.spyOn(console, "error").mockImplementation(() => {})

  await renderWithI18n(
    <ErrorBoundary>
      <ThrowError />
    </ErrorBoundary>,
  )

  expect(screen.getByText("エラーが発生しました")).toBeDefined()
  expect(screen.getByText("テストエラー")).toBeDefined()
  expect(screen.getByText("再試行")).toBeDefined()
})

test("renders children when no error", async () => {
  await renderWithI18n(
    <ErrorBoundary>
      <p>正常表示</p>
    </ErrorBoundary>,
  )

  expect(screen.getByText("正常表示")).toBeDefined()
})
