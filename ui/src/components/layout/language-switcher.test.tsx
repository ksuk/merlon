import { act, fireEvent, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, expect, test } from "vitest"
import { LANGUAGE_STORAGE_KEY } from "@/i18n"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { LanguageSwitcher } from "./language-switcher"

beforeEach(() => {
  localStorage.clear()
})

test("renders all supported languages", async () => {
  const { i18n } = await renderWithI18n(<LanguageSwitcher />)
  await act(() => i18n.changeLanguage("en"))

  const select = screen.getByRole("combobox")
  const options = within(select).getAllByRole("option")

  expect(options).toHaveLength(2)
  expect(options.map((o) => o.textContent)).toEqual(expect.arrayContaining(["English", "Japanese"]))
})

test("changes language without page reload", async () => {
  const { i18n } = await renderWithI18n(<LanguageSwitcher />)
  await act(() => i18n.changeLanguage("en"))

  const select = screen.getByRole("combobox")
  fireEvent.change(select, { target: { value: "ja" } })

  await waitFor(() => expect(screen.getByText("日本語")).toBeDefined())
  expect(i18n.language).toBe("ja")
})

test("persists selection to localStorage", async () => {
  const { i18n } = await renderWithI18n(<LanguageSwitcher />)
  await act(() => i18n.changeLanguage("en"))

  const select = screen.getByRole("combobox")
  fireEvent.change(select, { target: { value: "ja" } })

  await waitFor(() => expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("ja"))
})
