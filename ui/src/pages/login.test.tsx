import { screen, fireEvent, waitFor } from "@testing-library/react";
import { expect, test, vi, beforeEach } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { renderWithI18n } from "@/test/i18n-test-utils";
import { LoginPage } from "./login";

function renderWithRouter(ui: React.ReactElement) {
  return renderWithI18n(<MemoryRouter>{ui}</MemoryRouter>);
}

beforeEach(() => {
  vi.restoreAllMocks();
});

test("shows the Merlon brand", async () => {
  await renderWithRouter(<LoginPage />);

  expect(screen.getByRole("img", { name: "Merlon" })).toHaveAttribute(
    "src",
    "/logo.svg",
  );
});

test("submits credentials and calls the login API", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({ id: "u1", email: "alice@example.com", role: "admin" }),
      {
        status: 200,
      },
    ),
  );

  await renderWithRouter(<LoginPage />);

  fireEvent.change(screen.getByLabelText("メールアドレス"), {
    target: { value: "alice@example.com" },
  });
  fireEvent.change(screen.getByLabelText("パスワード"), {
    target: { value: "correct-horse-battery-staple" },
  });
  fireEvent.click(screen.getByRole("button", { name: "ログイン" }));

  await waitFor(() =>
    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({ method: "POST" }),
    ),
  );
});

test("shows an error message when login fails", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response("invalid email or password", { status: 401 }),
  );

  await renderWithRouter(<LoginPage />);

  fireEvent.change(screen.getByLabelText("メールアドレス"), {
    target: { value: "alice@example.com" },
  });
  fireEvent.change(screen.getByLabelText("パスワード"), {
    target: { value: "wrong-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: "ログイン" }));

  expect(
    await screen.findByText("メールアドレスまたはパスワードが正しくありません"),
  ).toBeDefined();
});

test("returns to the protected location that requested login", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({ id: "u1", email: "alice@example.com", role: "admin" }),
      {
        status: 200,
      },
    ),
  );

  await renderWithI18n(
    <MemoryRouter
      initialEntries={[
        {
          pathname: "/login",
          state: {
            from: {
              pathname: "/customers",
              search: "?risk=high",
              hash: "#results",
            },
          },
        },
      ]}
    >
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/customers" element={<p>元の顧客画面</p>} />
      </Routes>
    </MemoryRouter>,
  );

  fireEvent.change(screen.getByLabelText("メールアドレス"), {
    target: { value: "alice@example.com" },
  });
  fireEvent.change(screen.getByLabelText("パスワード"), {
    target: { value: "correct-horse-battery-staple" },
  });
  fireEvent.click(screen.getByRole("button", { name: "ログイン" }));

  expect(await screen.findByText("元の顧客画面")).toBeDefined();
});
