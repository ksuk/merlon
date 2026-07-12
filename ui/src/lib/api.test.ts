import { beforeEach, expect, test, vi } from "vitest"
import { api, ApiError } from "./api"

beforeEach(() => {
  vi.restoreAllMocks()
})

test("request throws ApiError with the server's error_code and message", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ error: "customer not found", error_code: "not_found" }), { status: 404 }),
  )

  await expect(api.customers.get("nonexistent")).rejects.toMatchObject({
    name: "ApiError",
    status: 404,
    code: "not_found",
    message: "customer not found",
  })
})

test("request falls back to the raw body when the error response is not JSON", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("upstream timeout", { status: 504 }))

  await expect(api.customers.get("x")).rejects.toMatchObject({
    name: "ApiError",
    status: 504,
    code: undefined,
    message: "upstream timeout",
  })
})

test("ApiError is an instance of Error", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify({ error: "forbidden", error_code: "forbidden" }), { status: 403 }),
  )

  try {
    await api.customers.get("x")
    throw new Error("expected rejection")
  } catch (err) {
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toBeInstanceOf(Error)
  }
})
