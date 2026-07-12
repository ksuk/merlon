import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type Alert } from "@/lib/api"
import { Download, FileText } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"

export function ReportsPage() {
  const { t } = useTranslation()
  const { data: alerts, loading, error } = useApi(api.alerts.list)
  const [result, setResult] = useState<{ id: string; alert_id: string } | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [selectedAlert, setSelectedAlert] = useState<Alert | null>(null)
  const pointRef = useRef<HTMLTextAreaElement>(null)
  const createdByRef = useRef<HTMLInputElement>(null)

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    if (!selectedAlert || !pointRef.current?.value.trim() || !createdByRef.current?.value.trim()) return
    setSubmitting(true)
    try {
      const report = await api.reports.createSTR(
        selectedAlert.id,
        pointRef.current.value.trim(),
        createdByRef.current.value.trim(),
      )
      setResult({ id: report.id, alert_id: report.alert_id })
      setSelectedAlert(null)
    } finally {
      setSubmitting(false)
    }
  }

  const escalatedAlerts = alerts?.filter(
    (a) => a.status === "escalated" || a.severity === "high" || a.severity === "critical",
  )

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">{t("reports.error")}</p>
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("reports.title")}</h1>

      {result && (
        <Card className="border-green-200 bg-green-50">
          <CardContent className="flex items-center justify-between p-4">
            <div>
              <p className="text-sm font-medium text-green-800">{t("reports.result.title")}</p>
              <p className="text-xs text-green-600">{t("reports.result.idLabel", { id: result.id })}</p>
            </div>
            <div className="flex gap-2">
              <a href={api.reports.exportSTR(result.alert_id, "csv")} download>
                <Button variant="outline" size="sm">
                  <Download className="h-4 w-4" />
                  {t("reports.export.csv")}
                </Button>
              </a>
              <a href={api.reports.exportSTR(result.alert_id, "json")} download>
                <Button variant="outline" size="sm">
                  <Download className="h-4 w-4" />
                  {t("reports.export.json")}
                </Button>
              </a>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <FileText className="h-4 w-4" />
            {t("reports.form.title")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleCreate} className="space-y-4">
            <div>
              <label className="mb-2 block text-sm font-medium">{t("reports.form.targetAlert")}</label>
              {escalatedAlerts && escalatedAlerts.length > 0 ? (
                <div className="max-h-48 space-y-2 overflow-y-auto">
                  {escalatedAlerts.map((a) => (
                    <button
                      key={a.id}
                      type="button"
                      onClick={() => setSelectedAlert(a)}
                      className={`flex w-full items-center justify-between rounded-md border p-3 text-left text-sm transition-colors ${
                        selectedAlert?.id === a.id
                          ? "border-primary bg-primary/5"
                          : "hover:bg-accent"
                      }`}
                    >
                      <div>
                        <span className="font-mono text-xs">{a.id.slice(0, 8)}</span>
                        <span className="ml-2">{a.description}</span>
                      </div>
                      <Badge variant={a.severity === "critical" ? "critical" : a.severity === "high" ? "high" : "medium"}>
                        {a.severity}
                      </Badge>
                    </button>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">{t("reports.form.noAlerts")}</p>
              )}
            </div>
            <div>
              <label className="mb-1 block text-sm font-medium">{t("reports.form.pointLabel")}</label>
              <textarea
                ref={pointRef}
                required
                rows={3}
                placeholder={t("reports.form.pointPlaceholder")}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            <div>
              <label className="mb-1 block text-sm font-medium">{t("reports.form.createdByLabel")}</label>
              <input
                ref={createdByRef}
                required
                placeholder={t("reports.form.createdByPlaceholder")}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            <Button type="submit" size="sm" disabled={submitting || !selectedAlert}>
              {t("reports.form.submit")}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
