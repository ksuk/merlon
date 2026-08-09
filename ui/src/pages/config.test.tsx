import { fireEvent, screen, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
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

const sampleRule = {
  id: "r1",
  type: "SCREENING_CONFIG",
  name: "sanctions_list",
  description: "",
  definition: { schema_version: "1.0" },
  version: 3,
  is_active: true,
  created_by: "u1",
  created_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-01T00:00:00Z",
}

function mockConfigAPI(validation?: unknown, baselineYAML = "list_id: sanctions\n") {
  const posted: { configType: string; yaml: string }[] = []
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString()
    if (url.includes("/export")) {
      return Promise.resolve(new Response(baselineYAML))
    }
    if (url.includes("/config/validate")) {
      const body = JSON.parse(String(init?.body))
      posted.push({ configType: body.config_type, yaml: body.yaml_content })
      return Promise.resolve(new Response(JSON.stringify(validation ?? { valid: true, errors: [] })))
    }
    if (url.includes("/rules")) {
      return Promise.resolve(
        new Response(JSON.stringify({ data: [sampleRule], pagination: { has_more: false } })),
      )
    }
    return Promise.resolve(new Response(JSON.stringify({})))
  })
  return posted
}

test("sends the config type the engine actually accepts", async () => {
  // The page used to offer "scenario_rules"; the engine switches on
  // "tm_scenarios", so that option could never validate.
  const posted = mockConfigAPI()

  await renderWithRouter(<ConfigPage />)

  fireEvent.click(screen.getByText("シナリオルール"))
  fireEvent.change(screen.getByLabelText("YAML"), { target: { value: "scenario_id: tm_structuring_basic" } })
  fireEvent.click(screen.getByText("検証"))

  await waitFor(() => expect(posted).toHaveLength(1))
  expect(posted[0].configType).toBe("tm_scenarios")
})

test("reports an empty submission instead of doing nothing", async () => {
  const posted = mockConfigAPI()

  await renderWithRouter(<ConfigPage />)

  fireEvent.click(screen.getByText("検証"))

  expect(await screen.findByRole("status")).toBeDefined()
  expect(screen.getByText(/何も送信されていません/)).toBeDefined()
  expect(posted).toHaveLength(0)
})

test("loads a baseline and reports an unchanged document as a no-op", async () => {
  const posted = mockConfigAPI()

  await renderWithRouter(<ConfigPage />)

  fireEvent.click(await screen.findByText("sanctions_list v3 を読み込む"))

  await screen.findByTestId("config-baseline")
  fireEvent.click(screen.getByText("検証"))

  expect(await screen.findByText(/変更は発生しません/)).toBeDefined()
  // An unchanged document is not sent: reporting "valid" for a submission that
  // changes nothing invites the operator to believe they saved something.
  expect(posted).toHaveLength(0)
})

test("shows the class and position of each finding", async () => {
  mockConfigAPI({
    valid: false,
    errors: [
      { field: "config", message: "list_id must not be empty", class: "schema", severity: "error", line: 1, column: 1, path: "list_id" },
    ],
    warnings: [
      { field: "config", message: "no entries will ever match", class: "schema", severity: "warning", path: "entries" },
    ],
  })

  await renderWithRouter(<ConfigPage />)

  fireEvent.change(screen.getByLabelText("YAML"), { target: { value: 'list_id: ""' } })
  fireEvent.click(screen.getByText("検証"))

  expect(await screen.findByText("list_id must not be empty")).toBeDefined()
  expect(screen.getAllByText("スキーマ").length).toBeGreaterThan(0)
  expect(screen.getByText("1 行 1 列")).toBeDefined()
  // A warning must be visibly separated from a rejection.
  expect(screen.getByText("警告は有効化をブロックしません。")).toBeDefined()
  expect(screen.getByText("no entries will ever match")).toBeDefined()
})
