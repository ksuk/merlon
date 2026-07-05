import { render, type RenderOptions } from "@testing-library/react"
import type { ReactElement } from "react"
import { I18nextProvider } from "react-i18next"
import i18n, { initI18n } from "@/i18n"

let readyPromise: Promise<unknown> | undefined

function ensureI18nReady() {
  if (!readyPromise) readyPromise = initI18n()
  return readyPromise
}

export async function renderWithI18n(ui: ReactElement, options?: RenderOptions) {
  await ensureI18nReady()
  const result = render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>, options)
  return { ...result, i18n }
}
