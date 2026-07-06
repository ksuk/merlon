import { render, type RenderOptions } from "@testing-library/react"
import type { ReactElement } from "react"
import { I18nextProvider } from "react-i18next"
import i18n, { changeLanguage, initI18n, type SupportedLanguage } from "@/i18n"

let readyPromise: Promise<unknown> | undefined

function ensureI18nReady() {
  if (!readyPromise) readyPromise = initI18n()
  return readyPromise
}

// Existing UI tests assert the original Japanese copy verbatim. Locking the
// test language to "ja" (rather than rewriting every assertion) lets those
// tests keep passing unchanged while pages are migrated to t() calls.
export async function renderWithI18n(
  ui: ReactElement,
  options?: RenderOptions & { language?: SupportedLanguage },
) {
  await ensureI18nReady()
  await changeLanguage(options?.language ?? "ja")
  const result = render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>, options)
  return { ...result, i18n }
}
