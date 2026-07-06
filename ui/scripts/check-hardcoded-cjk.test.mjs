import { expect, test } from "vitest"
import { detectHardcodedCJK } from "./check-hardcoded-cjk.mjs"

test("detectHardcodedCJK finds CJK in JSX text content", () => {
  const hits = detectHardcodedCJK("src/pages/example.tsx", "export const X = () => <p>顧客</p>\n")
  expect(hits.length).toBe(1)
  expect(hits[0].line).toBe(1)
})

test("detectHardcodedCJK finds CJK in string literals", () => {
  const hits = detectHardcodedCJK("src/pages/example.tsx", 'const label = "取引"\n')
  expect(hits.length).toBe(1)
  expect(hits[0].line).toBe(1)
})

test("detectHardcodedCJK ignores line comments", () => {
  const hits = detectHardcodedCJK("src/pages/example.tsx", "// 日本語コメント\nconst x = 1\n")
  expect(hits.length).toBe(0)
})

test("detectHardcodedCJK ignores block comments", () => {
  const hits = detectHardcodedCJK("src/pages/example.tsx", "/*\n 日本語コメント\n*/\nconst x = 1\n")
  expect(hits.length).toBe(0)
})

test("detectHardcodedCJK ignores i18n catalog files", () => {
  const hits = detectHardcodedCJK("src/i18n/locales/ja/common.json", '{"title": "顧客"}')
  expect(hits.length).toBe(0)
})

test("detectHardcodedCJK does not special-case test files", () => {
  const hits = detectHardcodedCJK("src/pages/example.test.tsx", 'expect(screen.getByText("顧客")).toBeDefined()\n')
  expect(hits.length).toBe(1)
})

test("detectHardcodedCJK respects an i18n-ignore trailing comment", () => {
  const hits = detectHardcodedCJK("src/pages/example.tsx", 'const label = "顧客" // i18n-ignore\n')
  expect(hits.length).toBe(0)
})

test("detectHardcodedCJK reports file path and line number", () => {
  const hits = detectHardcodedCJK("src/pages/example.tsx", "const a = 1\nconst b = \"取引\"\n")
  expect(hits).toEqual([
    { file: "src/pages/example.tsx", line: 2, text: 'const b = "取引"' },
  ])
})

test("detectHardcodedCJK returns empty array for non-CJK source", () => {
  const hits = detectHardcodedCJK("src/pages/example.tsx", 'const label = t("cases.title")\n')
  expect(hits.length).toBe(0)
})
