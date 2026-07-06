import { useTranslation } from "react-i18next"
import { Link } from "react-router-dom"

export function NotFoundPage() {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-[400px] items-center justify-center">
      <div className="text-center">
        <p className="text-6xl font-bold text-muted-foreground/30">404</p>
        <h2 className="mt-4 text-lg font-semibold">{t("notFound.title")}</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("notFound.description")}
        </p>
        <Link
          to="/"
          className="mt-6 inline-block rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90"
        >
          {t("notFound.backToDashboard")}
        </Link>
      </div>
    </div>
  )
}
