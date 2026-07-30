import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeAll, beforeEach, expect, test, vi } from "vitest";
import { AuthGate } from "@/components/auth-gate";
import { changeLanguage, initI18n } from "@/i18n";
import App from "./App";

vi.mock("@/pages/dashboard", () => ({
  DashboardPage: () => <div>dashboard sentinel</div>,
}));

const systemInfo = {
  version: "test",
  components: ["api"],
  endpoints: 1,
  features: { auth: true, demo_data: false },
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mockAuthenticatedAPI() {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.endsWith("/system/info")) return jsonResponse(systemInfo);
    return jsonResponse({});
  });
}

beforeAll(async () => {
  await initI18n();
  await changeLanguage("ja");
});

beforeEach(() => {
  window.history.replaceState(null, "", "/");
});

afterEach(() => {
  vi.restoreAllMocks();
});

test("renders the protected dashboard after the session probe succeeds", async () => {
  mockAuthenticatedAPI();

  render(<App />);

  expect(await screen.findByText("Merlon")).toBeDefined();
  expect(await screen.findByText("dashboard sentinel")).toBeDefined();
  expect(screen.getByText("ダッシュボード")).toBeDefined();
  expect(screen.getByText("顧客")).toBeDefined();
  expect(screen.getAllByText("アラート").length).toBeGreaterThanOrEqual(1);
  expect(screen.getByText("ケース")).toBeDefined();
  expect(screen.getByText("取引")).toBeDefined();
  expect(screen.getByText("監査ログ")).toBeDefined();
  expect(screen.getByText("システム")).toBeDefined();
});

test("redirects an unauthenticated protected route to login before mounting the layout", async () => {
  window.history.replaceState(null, "", "/customers?risk=high#results");
  const fetchSpy = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/system/info")) {
        return jsonResponse(
          { error: "missing Authorization header", error_code: "unauthorized" },
          401,
        );
      }
      if (url.endsWith("/auth/refresh")) {
        return jsonResponse(
          { error: "missing refresh token", error_code: "unauthorized" },
          401,
        );
      }
      throw new Error(`unexpected request: ${url}`);
    });

  render(<App />);

  await waitFor(() => expect(window.location.pathname).toBe("/login"));
  expect(
    (await screen.findAllByText("ログイン")).length,
  ).toBeGreaterThanOrEqual(1);
  expect(screen.queryByText("Merlon")).toBeNull();
  expect(fetchSpy).toHaveBeenCalledWith(
    "/api/v1/auth/refresh",
    expect.objectContaining({ method: "POST" }),
  );
});

test("uses a valid refresh token before deciding the user is unauthenticated", async () => {
  window.history.replaceState(null, "", "/definitely-missing");
  let systemInfoCalls = 0;
  const fetchSpy = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/system/info")) {
        systemInfoCalls++;
        if (systemInfoCalls === 1) {
          return jsonResponse(
            { error: "expired", error_code: "unauthorized" },
            401,
          );
        }
        return jsonResponse(systemInfo);
      }
      if (url.endsWith("/auth/refresh")) {
        return jsonResponse({ status: "refreshed" });
      }
      return jsonResponse({});
    });

  render(<App />);

  expect(await screen.findByText("Merlon")).toBeDefined();
  expect(window.location.pathname).toBe("/definitely-missing");
  expect(fetchSpy).toHaveBeenCalledWith(
    "/api/v1/auth/refresh",
    expect.objectContaining({ method: "POST" }),
  );
  expect(systemInfoCalls).toBeGreaterThanOrEqual(2);
});

test("keeps the authentication-disabled demo topology usable", async () => {
  window.history.replaceState(null, "", "/definitely-missing");
  const fetchSpy = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/system/info")) {
        return jsonResponse({
          ...systemInfo,
          features: { ...systemInfo.features, auth: false, demo_data: true },
        });
      }
      return jsonResponse({});
    });

  render(<App />);

  expect(await screen.findByText("Merlon")).toBeDefined();
  expect(screen.queryByRole("heading", { name: "ログイン" })).toBeNull();
  expect(
    fetchSpy.mock.calls.some(([input]) =>
      String(input).endsWith("/auth/refresh"),
    ),
  ).toBe(false);
});

test("shows a retryable error instead of treating an API outage as logout", async () => {
  let available = false;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url.endsWith("/system/info")) {
      if (!available) {
        return jsonResponse(
          { error: "unavailable", error_code: "service_unavailable" },
          503,
        );
      }
      return jsonResponse(systemInfo);
    }
    return jsonResponse({});
  });

  render(
    <MemoryRouter initialEntries={["/protected"]}>
      <Routes>
        <Route path="/login" element={<div>login sentinel</div>} />
        <Route element={<AuthGate />}>
          <Route path="/protected" element={<div>protected sentinel</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

  expect(
    await screen.findByText("セッションを確認できませんでした"),
  ).toBeDefined();
  expect(screen.queryByText("login sentinel")).toBeNull();

  available = true;
  fireEvent.click(screen.getByRole("button", { name: "再試行" }));

  await waitFor(() =>
    expect(screen.getByText("protected sentinel")).toBeDefined(),
  );
});
