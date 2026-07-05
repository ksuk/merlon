import i18n from "i18next"
import LanguageDetector from "i18next-browser-languagedetector"
import { initReactI18next } from "react-i18next"

export const SUPPORTED_LANGUAGES = ["en", "ja"] as const
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number]
// I18N-002: the bundled default language is English; ja is auto-selected via
// the browser language detector when no explicit preference is stored.
export const DEFAULT_LANGUAGE: SupportedLanguage = "en"

export const LANGUAGE_STORAGE_KEY = "merlon_language"

// I18N-003: adding a language requires only a new entry here and a matching
// locales/{lang}/common.json — catalogs are dynamically imported so unused
// language bundles never ship to the client.
const catalogLoaders: Record<SupportedLanguage, () => Promise<{ default: Record<string, unknown> }>> = {
  en: () => import("./locales/en/common.json"),
  ja: () => import("./locales/ja/common.json"),
}

function isSupportedLanguage(lang: string): lang is SupportedLanguage {
  return (SUPPORTED_LANGUAGES as readonly string[]).includes(lang)
}

async function loadCatalog(lang: SupportedLanguage): Promise<void> {
  if (i18n.hasResourceBundle(lang, "common")) return
  const { default: catalog } = await catalogLoaders[lang]()
  i18n.addResourceBundle(lang, "common", catalog)
}

i18n.on("languageChanged", (lng) => {
  void loadCatalog(isSupportedLanguage(lng) ? lng : DEFAULT_LANGUAGE)
})

// Loads the target catalog before switching so react-i18next's
// languageChanged re-render already has the translations available
// (addResourceBundle alone does not trigger a re-render).
export async function changeLanguage(lang: SupportedLanguage): Promise<void> {
  await loadCatalog(lang)
  await i18n.changeLanguage(lang)
}

export async function initI18n(): Promise<typeof i18n> {
  await i18n
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
      supportedLngs: SUPPORTED_LANGUAGES,
      fallbackLng: DEFAULT_LANGUAGE,
      ns: ["common"],
      defaultNS: "common",
      resources: {},
      detection: {
        order: ["localStorage", "navigator"],
        lookupLocalStorage: LANGUAGE_STORAGE_KEY,
        caches: ["localStorage"],
      },
      interpolation: { escapeValue: false },
    })

  const resolved = isSupportedLanguage(i18n.language) ? i18n.language : DEFAULT_LANGUAGE
  await loadCatalog(resolved)

  return i18n
}

export default i18n
