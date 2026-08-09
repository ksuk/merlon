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
  // The thrown message is a developer artifact and may quote internal detail,
  // so the operator gets a stable explanation and a way forward instead.
  expect(screen.queryByText("テストエラー")).toBeNull()
  expect(screen.getByText(/この画面を表示できませんでした/)).toBeDefined()
  expect(screen.getByText("再試行")).toBeDefined()
  expect(screen.getByText("ダッシュボードへ")).toBeDefined()
})

test("the failure is reported rather than swallowed", async () => {
  const logged = vi.spyOn(console, "error").mockImplementation(() => {})

  await renderWithI18n(
    <ErrorBoundary>
      <ThrowError />
    </ErrorBoundary>,
  )

  // The boundary used to log nothing at all, so a reproducible crash left no
  // trace for anyone to investigate.
  expect(logged.mock.calls.some((call) => String(call[0]).includes("UI render failure"))).toBe(true)
})

test("navigating away clears a failed screen", async () => {
  vi.spyOn(console, "error").mockImplementation(() => {})

  const { rerender } = await renderWithI18n(
    <ErrorBoundary resetKey="/alerts">
      <ThrowError />
    </ErrorBoundary>,
  )
  expect(screen.getByText("エラーが発生しました")).toBeDefined()

  rerender(
    <ErrorBoundary resetKey="/cases">
      <p>次の画面</p>
    </ErrorBoundary>,
  )

  expect(screen.getByText("次の画面")).toBeDefined()
})

test("renders children when no error", async () => {
  await renderWithI18n(
    <ErrorBoundary>
      <p>正常表示</p>
    </ErrorBoundary>,
  )

  expect(screen.getByText("正常表示")).toBeDefined()
})
