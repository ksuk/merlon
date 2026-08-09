import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type ComponentStatus, type OperationalState, type SystemStatus } from "@/lib/api"
import { AlertTriangle, CheckCircle, CircleHelp, RefreshCw, XCircle } from "lucide-react"
import { useCallback, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

// A green check is reserved for a check that ran and succeeded. Configured,
// stale, unknown and failed each get their own mark, because the page's whole
// purpose is that an operator can tell them apart (#83).
const STATE_PRESENTATION: Record<
  OperationalState,
  { icon: typeof CheckCircle; className: string; badge: "low" | "outline" | "destructive" | "secondary" }
> = {
  ready: { icon: CheckCircle, className: "text-green-600", badge: "low" },
  degraded: { icon: AlertTriangle, className: "text-amber-600", badge: "outline" },
  unavailable: { icon: XCircle, className: "text-red-600", badge: "destructive" },
  unknown: { icon: CircleHelp, className: "text-muted-foreground", badge: "secondary" },
}

// Components an operator can act on link to the screen that shows the work.
const COMPONENT_DESTINATIONS: Record<string, string> = {
  screening_sources: "/screening-queue",
}

function ComponentRow({ component }: { component: ComponentStatus }) {
  const { t } = useTranslation()
  const presentation = STATE_PRESENTATION[component.operational_state] ?? STATE_PRESENTATION.unknown
  const Icon = presentation.icon
  const destination = COMPONENT_DESTINATIONS[component.name]

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-b py-2 last:border-b-0">
      <div className="flex items-center gap-2">
        <Icon className={`h-4 w-4 ${presentation.className}`} aria-hidden="true" />
        <span className="text-sm">
          {t(`system.components.${component.name}`, { defaultValue: component.name })}
        </span>
        {!component.configured && (
          <Badge variant="secondary">{t("system.status.notConfigured")}</Badge>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {component.reason_code && (
          <span className="text-xs text-muted-foreground">
            {t(`system.reason.${component.reason_code}`, { defaultValue: component.reason_code })}
          </span>
        )}
        {destination && component.operational_state !== "ready" && (
          <Link to={destination} className="text-xs underline">
            {t("system.components.investigate")}
          </Link>
        )}
        <Badge variant={presentation.badge}>
          {t(`system.operationalState.${component.operational_state}`, {
            defaultValue: component.operational_state,
          })}
        </Badge>
      </div>
    </div>
  )
}

export function SystemPage() {
  const { t, i18n } = useTranslation()
  const [refreshKey, setRefreshKey] = useState(0)

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
    demo_data: t("system.features.demo_data"),
  }

  const { data: info, error: infoError } = useApi(api.system.info)
  const {
    data: status,
    error: statusError,
    loading: statusLoading,
  } = useApi<SystemStatus>(
    // refreshKey > 0 means the operator asked, so the cache is bypassed.
    useCallback(() => api.system.status(refreshKey > 0), [refreshKey]),
    refreshKey,
  )

  // Partial failure must not discard the rest of the page: a readiness lookup
  // that failed is itself information, and the feature list is still true.
  if (infoError && statusError) {
    return (
      <p role="alert" className="p-12 text-center text-destructive">
        {t("system.error")}
      </p>
    )
  }

  const features = info?.features ?? {}
  const enabledCount = Object.values(features).filter(Boolean).length
  const totalCount = Object.keys(features).length

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-bold tracking-tight">{t("system.title")}</h1>
        <div className="flex items-center gap-3">
          {status && (
            <span className="text-xs text-muted-foreground" data-testid="status-freshness">
              {t(`system.freshness.${status.source}`, {
                time: new Date(status.checked_at).toLocaleString(i18n.language),
              })}
            </span>
          )}
          <Button size="sm" variant="outline" onClick={() => setRefreshKey((v) => v + 1)} disabled={statusLoading}>
            <RefreshCw className={`mr-2 h-4 w-4 ${statusLoading ? "animate-spin" : ""}`} aria-hidden="true" />
            {t("system.refresh")}
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{t("system.stats.version")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">v{status?.version ?? info?.version ?? "-"}</p>
            {status?.commit ? (
              <p className="mt-1 font-mono text-xs text-muted-foreground">{status.commit}</p>
            ) : (
              <p className="mt-1 text-xs text-muted-foreground">{t("system.build.commitUnknown")}</p>
            )}
            {status?.built_at && (
              <p className="text-xs text-muted-foreground">
                {t("system.build.builtAt", { date: status.built_at })}
              </p>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{t("system.stats.endpoints")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{info?.endpoints ?? "-"}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              {t("system.stats.enabledFeatures")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">
              {enabledCount}/{totalCount}
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("system.components.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          {statusError ? (
            <p role="alert" className="text-sm text-destructive">
              {t("system.statusUnavailable")}
            </p>
          ) : status ? (
            <div>
              {status.components.map((component) => (
                <ComponentRow key={component.name} component={component} />
              ))}
            </div>
          ) : (
            <div className="h-24 animate-pulse rounded bg-muted" />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("system.provenance.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-xs text-muted-foreground">{t("system.provenance.help")}</p>

          {status ? (
            <>
              <div className="flex flex-wrap gap-4 text-sm">
                <div>
                  <span className="text-xs text-muted-foreground">{t("system.provenance.authMode")}: </span>
                  {t(`layout.session.authMode.${status.auth_mode === "api_key_only" ? "apiKeyOnly" : status.auth_mode}`, {
                    defaultValue: status.auth_mode,
                  })}
                </div>
                {status.base_currency && (
                  <div>
                    <span className="text-xs text-muted-foreground">{t("system.provenance.baseCurrency")}: </span>
                    {status.base_currency}
                  </div>
                )}
              </div>

              <div>
                <p className="mb-1 text-xs font-medium">{t("system.provenance.configDigests")}</p>
                {Object.keys(status.config_digests).length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t("system.provenance.noDigests")}</p>
                ) : (
                  <dl className="grid gap-1 sm:grid-cols-[14rem_1fr]">
                    {Object.entries(status.config_digests).map(([name, digest]) => (
                      <div key={name} className="contents">
                        <dt className="font-mono text-xs text-muted-foreground">{name}</dt>
                        <dd className="break-all font-mono text-xs">{digest}</dd>
                      </div>
                    ))}
                  </dl>
                )}
              </div>

              <div>
                <p className="mb-1 text-xs font-medium">{t("system.provenance.policies")}</p>
                <ul className="space-y-1">
                  {status.policies.map((policy) => (
                    <li key={policy.name} className="flex flex-wrap items-center gap-2 text-xs">
                      <span className="font-mono">{policy.name}</span>
                      <span>{policy.policy_version}</span>
                      <Badge variant={policy.source === "file" ? "outline" : "secondary"}>
                        {t(`system.provenance.source.${policy.source}`)}
                      </Badge>
                      <span className="break-all font-mono text-muted-foreground">{policy.digest.slice(0, 12)}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">{t("system.statusUnavailable")}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("system.features.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="mb-2 text-xs text-muted-foreground">{t("system.features.help")}</p>
          <div className="space-y-2">
            {Object.entries(features).map(([key, enabled]) => (
              <div key={key} className="flex items-center justify-between">
                <span className="text-sm">{featureLabels[key] ?? key}</span>
                <Badge variant={enabled ? "outline" : "secondary"}>
                  {enabled ? t("system.status.enabled") : t("system.status.disabled")}
                </Badge>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
