import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { SetupPage } from "./setup"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("submits the initial admin account creation form", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ id: "u1", email: "admin@example.com", role: "admin" }), {
      status: 201,
    }),
  )

  renderWithRouter(<SetupPage />)

  fireEvent.change(screen.getByLabelText("メールアドレス"), { target: { value: "admin@example.com" } })
  fireEvent.change(screen.getByLabelText("初期パスワード（12文字以上）"), {
    target: { value: "correct-horse-battery-staple" },
  })
  fireEvent.click(screen.getByRole("button", { name: "管理者アカウントを作成" }))

  await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith(
    "/api/v1/setup",
    expect.objectContaining({ method: "POST" }),
  ))
})

test("shows an error message when setup has already completed", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response("initial setup has already been completed", { status: 409 }),
  )

  renderWithRouter(<SetupPage />)

  fireEvent.change(screen.getByLabelText("メールアドレス"), { target: { value: "admin@example.com" } })
  fireEvent.change(screen.getByLabelText("初期パスワード（12文字以上）"), {
    target: { value: "correct-horse-battery-staple" },
  })
  fireEvent.click(screen.getByRole("button", { name: "管理者アカウントを作成" }))

  expect(
    await screen.findByText("初期セットアップに失敗しました。既に管理者アカウントが作成済みの可能性があります"),
  ).toBeDefined()
})
