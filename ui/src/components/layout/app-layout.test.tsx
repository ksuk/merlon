import { screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, expect, test, vi } from "vitest"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { AppLayout } from "./app-layout"

beforeEach(() => {
  vi.restoreAllMocks()
})

function renderLayout() {
  return renderWithI18n(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<div>page content</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

test("does not show the synthetic demo data badge when the flag is off", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        version: "1.0.0",
        components: ["api", "engine", "database"],
        endpoints: 36,
        features: { scoring: true, demo_data: false },
      }),
    ),
  )

  await renderLayout()

  await waitFor(() => expect(screen.getByText("page content")).toBeDefined())
  expect(screen.queryByText("合成デモデータ")).toBeNull()
})

test("shows the Merlon brand in the sidebar", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  await renderLayout()

  expect(await screen.findByRole("img", { name: "Merlon" })).toHaveAttribute(
    "src",
    "/logo.svg",
  )
})

test("shows the synthetic demo data badge when features.demo_data is true", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        version: "1.0.0",
        components: ["api", "engine", "database"],
        endpoints: 36,
        features: { scoring: true, demo_data: true },
      }),
    ),
  )

  await renderLayout()

  expect(await screen.findByText("合成デモデータ")).toBeDefined()
})

test("does not show the badge when the system info request fails", async () => {
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("fail"))

  await renderLayout()

  await waitFor(() => expect(screen.getByText("page content")).toBeDefined())
  expect(screen.queryByText("合成デモデータ")).toBeNull()
})
