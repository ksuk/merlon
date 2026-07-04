import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { LoginPage } from "./login"

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("submits credentials and calls the login API", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ id: "u1", email: "alice@example.com", role: "admin" }), {
      status: 200,
    }),
  )

  renderWithRouter(<LoginPage />)

  fireEvent.change(screen.getByLabelText("メールアドレス"), { target: { value: "alice@example.com" } })
  fireEvent.change(screen.getByLabelText("パスワード"), { target: { value: "correct-horse-battery-staple" } })
  fireEvent.click(screen.getByRole("button", { name: "ログイン" }))

  await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith(
    "/api/v1/auth/login",
    expect.objectContaining({ method: "POST" }),
  ))
})

test("shows an error message when login fails", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("invalid email or password", { status: 401 }))

  renderWithRouter(<LoginPage />)

  fireEvent.change(screen.getByLabelText("メールアドレス"), { target: { value: "alice@example.com" } })
  fireEvent.change(screen.getByLabelText("パスワード"), { target: { value: "wrong-password" } })
  fireEvent.click(screen.getByRole("button", { name: "ログイン" }))

  expect(await screen.findByText("メールアドレスまたはパスワードが正しくありません")).toBeDefined()
})
