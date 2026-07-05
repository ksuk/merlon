import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
import { CheckCircle, XCircle } from "lucide-react"
import { useTranslation } from "react-i18next"

export function SystemPage() {
  const { t } = useTranslation()
  const featureLabels: Record<string, string> = {
    auth: t("system.features.auth"),
    audit: t("system.features.audit"),
    cases: t("system.features.cases"),
    webhooks: t("system.features.webhooks"),
    rate_limit: t("system.features.rate_limit"),
    scoring: t("system.features.scoring"),
    monitoring: t("system.features.monitoring"),
    screening: t("system.features.screening"),
    backtest: t("system.features.backtest"),
    config: t("system.features.config"),
  }
  const componentLabels: Record<string, string> = {
    api: t("system.components.api"),
    engine: t("system.components.engine"),
    database: t("system.components.database"),
  }
  const { data: info, loading, error } = useApi(api.system.info)

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="grid gap-4 md:grid-cols-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <div key={i} className="h-48 animate-pulse rounded-xl border bg-muted" />
          ))}
        </div>
      </div>
    )
  }

  if (error || !info) {
    return <p className="p-12 text-center text-destructive">{t("system.error")}</p>
  }

  const enabledCount = Object.values(info.features).filter(Boolean).length
  const totalCount = Object.keys(info.features).length

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("system.title")}</h1>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{t("system.stats.version")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">v{info.version}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{t("system.stats.endpoints")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{info.endpoints}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{t("system.stats.enabledFeatures")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{enabledCount}/{totalCount}</p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("system.components.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {info.components.map((c) => (
                <div key={c} className="flex items-center gap-2">
                  <CheckCircle className="h-4 w-4 text-green-600" />
                  <span className="text-sm">{componentLabels[c] ?? c}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("system.features.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {Object.entries(info.features).map(([key, enabled]) => (
                <div key={key} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    {enabled ? (
                      <CheckCircle className="h-4 w-4 text-green-600" />
                    ) : (
                      <XCircle className="h-4 w-4 text-muted-foreground" />
                    )}
                    <span className="text-sm">{featureLabels[key] ?? key}</span>
                  </div>
                  <Badge variant={enabled ? "low" : "secondary"}>
                    {enabled ? t("system.status.enabled") : t("system.status.disabled")}
                  </Badge>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
