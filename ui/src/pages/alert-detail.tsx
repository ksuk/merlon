import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type AlertSeverity, type AlertStatus } from "@/lib/api"
import { ArrowLeft } from "lucide-react"
import { useCallback, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router-dom"

const SEVERITY_VARIANT: Record<AlertSeverity, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function AlertDetailPage() {
  const { t, i18n } = useTranslation()
  const severityLabels: Record<string, string> = {
    low: t("alertSeverity.low"),
    medium: t("alertSeverity.medium"),
    high: t("alertSeverity.high"),
    critical: t("alertSeverity.critical"),
  }
  const statusLabels: Record<AlertStatus, string> = {
    open: t("alertStatus.open"),
    investigating: t("alertStatus.investigating"),
    escalated: t("alertStatus.escalated"),
    closed_true_positive: t("alertStatus.closed_true_positive"),
    closed_false_positive: t("alertStatus.closed_false_positive"),
  }
  const statusTransitions: Record<AlertStatus, { label: string; value: AlertStatus }[]> = {
    open: [
      { label: t("alertDetail.transitions.startInvestigation"), value: "investigating" },
      { label: t("alertDetail.transitions.escalate"), value: "escalated" },
    ],
    investigating: [
      { label: t("alertDetail.transitions.escalate"), value: "escalated" },
      { label: t("alertDetail.transitions.closeTruePositive"), value: "closed_true_positive" },
      { label: t("alertDetail.transitions.closeFalsePositive"), value: "closed_false_positive" },
    ],
    escalated: [
      { label: t("alertDetail.transitions.closeTruePositive"), value: "closed_true_positive" },
      { label: t("alertDetail.transitions.closeFalsePositive"), value: "closed_false_positive" },
    ],
    closed_true_positive: [],
    closed_false_positive: [],
  }
  const { id } = useParams<{ id: string }>()
  const { data: alert, loading, error } = useApi(
    useCallback(() => api.alerts.get(id!), [id]),
  )
  const [updating, setUpdating] = useState(false)

  async function handleStatusChange(status: AlertStatus) {
    if (!id) return
    setUpdating(true)
    try {
      await api.alerts.updateStatus(id, status)
      window.location.reload()
    } catch {
      setUpdating(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-64 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error || !alert) {
    return (
      <div className="space-y-4">
        <Link to="/alerts" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("alertDetail.backToList")}
        </Link>
        <p className="text-destructive">{t("alertDetail.error")}</p>
      </div>
    )
  }

  const transitions = statusTransitions[alert.status] ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/alerts" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("alertDetail.back")}
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{t("alertDetail.title")}</h1>
        <Badge variant={SEVERITY_VARIANT[alert.severity]}>
          {severityLabels[alert.severity]}
        </Badge>
        <Badge variant="outline">{statusLabels[alert.status]}</Badge>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("alertDetail.info.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">ID</dt>
                <dd className="font-mono">{alert.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.customerId")}</dt>
                <dd>
                  <Link to={`/customers/${alert.customer_id}`} className="font-mono text-primary underline-offset-4 hover:underline">
                    {alert.customer_id}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.scenario")}</dt>
                <dd className="font-mono">{alert.scenario_id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.score")}</dt>
                <dd className="text-lg font-bold">{alert.score.toFixed(1)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.detectedAt")}</dt>
                <dd>{formatDateTime(alert.detected_at, i18n.language)}</dd>
              </div>
              {alert.resolved_at && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("alertDetail.info.resolvedAt")}</dt>
                  <dd>{formatDateTime(alert.resolved_at, i18n.language)}</dd>
                </div>
              )}
              {alert.resolved_by && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("alertDetail.info.resolvedBy")}</dt>
                  <dd>{alert.resolved_by}</dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("alertDetail.description.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{alert.description}</p>
            {alert.transaction_ids.length > 0 && (
              <div className="mt-4">
                <p className="mb-2 text-xs font-medium text-muted-foreground">{t("alertDetail.description.relatedTransactions")}</p>
                <div className="flex flex-wrap gap-1">
                  {alert.transaction_ids.map((tid) => (
                    <Badge key={tid} variant="secondary" className="font-mono text-xs">
                      {tid}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {transitions.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("alertDetail.transitions.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex gap-2">
              {transitions.map((transition) => (
                <Button
                  key={transition.value}
                  variant={transition.value.startsWith("closed") ? "destructive" : "outline"}
                  size="sm"
                  disabled={updating}
                  onClick={() => handleStatusChange(transition.value)}
                >
                  {transition.label}
                </Button>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
