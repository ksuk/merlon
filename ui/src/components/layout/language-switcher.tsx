import { useTranslation } from "react-i18next"
import { changeLanguage, SUPPORTED_LANGUAGES, type SupportedLanguage } from "@/i18n"

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation()

  return (
    <select
      aria-label={t("language.switcherLabel")}
      value={i18n.language}
      onChange={(event) => {
        void changeLanguage(event.target.value as SupportedLanguage)
      }}
      className="rounded-md border bg-background px-2 py-1 text-sm text-foreground"
    >
      {SUPPORTED_LANGUAGES.map((lang) => (
        <option key={lang} value={lang}>
          {t(`language.${lang}`)}
        </option>
      ))}
    </select>
  )
}
