import { beforeAll, expect, test } from "vitest"
import i18n, { initI18n } from "@/i18n"
import { ApiError } from "./api"
import { translateApiError } from "./errors"

beforeAll(async () => {
  await initI18n()
})

test("translateApiError resolves a known error_code via the errors.* catalog", () => {
  const err = new ApiError("customer not found", 404, "not_found")
  expect(translateApiError(err, i18n.t)).toBe(i18n.t("errors.not_found"))
})

test("translateApiError falls back to the server message for an unrecognized error_code", () => {
  const err = new ApiError("a brand new failure mode", 400, "some_future_code")
  expect(translateApiError(err, i18n.t)).toBe("a brand new failure mode")
})

test("translateApiError falls back to the message when error_code is absent", () => {
  const err = new ApiError("upstream timeout", 504)
  expect(translateApiError(err, i18n.t)).toBe("upstream timeout")
})

test("translateApiError handles a plain Error", () => {
  expect(translateApiError(new Error("network error"), i18n.t)).toBe("network error")
})

test("translateApiError falls back to errors.unknown for a non-Error value", () => {
  expect(translateApiError("not an error", i18n.t)).toBe(i18n.t("errors.unknown"))
})
