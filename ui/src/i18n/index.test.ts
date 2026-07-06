import { beforeEach, expect, test } from "vitest"
import en from "./locales/en/common.json"
import ja from "./locales/ja/common.json"
import { LANGUAGE_STORAGE_KEY, initI18n } from "./index"

function collectKeyPaths(obj: Record<string, unknown>, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === "object" && !Array.isArray(value)) {
      return collectKeyPaths(value as Record<string, unknown>, path)
    }
    return [path]
  })
}

beforeEach(() => {
  localStorage.clear()
})

test("common.json ja and en have identical key sets", () => {
  expect(collectKeyPaths(ja).sort()).toEqual(collectKeyPaths(en).sort())
})

test("initI18n loads with default language en", async () => {
  const i18n = await initI18n()
  expect(i18n.language).toBe("en")
})

test("initI18n falls back to en for unsupported language", async () => {
  localStorage.setItem(LANGUAGE_STORAGE_KEY, "fr")
  const i18n = await initI18n()
  expect(i18n.language).toBe("en")
})
