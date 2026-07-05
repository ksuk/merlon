import { render, screen } from "@testing-library/react"
import { expect, test } from "vitest"
import { RuleDiffView } from "./rule-diff-view"

test("renders before/after values side by side", () => {
  const diff = JSON.stringify({
    threshold: { before: 1000000, after: 2000000 },
    is_active: { after: true },
  })

  render(<RuleDiffView details={{ diff }} />)

  expect(screen.getByText("threshold")).toBeDefined()
  expect(screen.getByText("1000000")).toBeDefined()
  expect(screen.getByText("2000000")).toBeDefined()
  expect(screen.getByText("is_active")).toBeDefined()
})

test("shows a message when there is no diff", () => {
  render(<RuleDiffView details={{}} />)

  expect(screen.getByText("差分情報がありません")).toBeDefined()
})

test("shows a message when the diff cannot be parsed", () => {
  render(<RuleDiffView details={{ diff: "not-json" }} />)

  expect(screen.getByText("差分の解析に失敗しました")).toBeDefined()
})
