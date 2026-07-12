import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, type Alert, type AlertSeverity, type AlertStatus } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router-dom"

const SEVERITY_VARIANT: Record<AlertSeverity, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function AlertsPage() {
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
  const [alerts, setAlerts] = useState<Alert[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // 一括ケース統合（the case-management workflow §アラートの一括処理）: 選択済みアラート
  // の ID をそのまま bulk-case へ渡す（既存ケースに追加、または新規ケースと
  // してまとめる）。
  const [selected, setSelected] = useState<Set<string>>(new Set())

  // 一括クローズ（the case-management workflow §アラートの一括処理）: bulk-close は
  // シナリオID・期間・severity の「フィルタ条件」でアラートを絞り込んで
  // CLOSED にする API であり、個別のアラートID指定ではない。そのため UI も
  // チェックボックス選択とは独立したフィルタ入力フォームとする。
  const [closeScenarioId, setCloseScenarioId] = useState("")
  const [closeSeverity, setCloseSeverity] = useState<AlertSeverity | "">("")
  const [closeReason, setCloseReason] = useState("")

  async function reload() {
    setLoading(true)
    try {
      const data = await api.alerts.list()
      setAlerts(data)
      setError(null)
    } catch (err) {
      setError(translateApiError(err, t))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function toggleSelected(id: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  async function handleBulkClose() {
    if (!closeReason.trim()) return
    setBusy(true)
    setActionError(null)
    try {
      await api.alerts.bulkClose({
        scenario_id: closeScenarioId.trim() || undefined,
        severity: closeSeverity || undefined,
        reason: closeReason.trim(),
      })
      setCloseScenarioId("")
      setCloseSeverity("")
      setCloseReason("")
      await reload()
    } catch (err) {
      setActionError(translateApiError(err, t))
    } finally {
      setBusy(false)
    }
  }

  async function handleBulkCase() {
    if (selected.size === 0) return
    const alertIds = [...selected]
    const customerId = alerts?.find((a) => a.id === alertIds[0])?.customer_id
    if (!customerId) return

    setBusy(true)
    setActionError(null)
    try {
      await api.alerts.bulkCase({ alert_ids: alertIds, customer_id: customerId })
      setSelected(new Set())
      await reload()
    } catch (err) {
      setActionError(translateApiError(err, t))
    } finally {
      setBusy(false)
    }
  }

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">{t("alerts.error")}</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("alerts.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("alerts.count", { count: alerts?.length ?? 0 })}</p>
      </div>

      <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-muted/40 p-4">
        <span className="text-sm font-medium">{t("alerts.bulkClose.label")}</span>
        <input
          type="text"
          placeholder={t("alerts.bulkClose.scenarioIdPlaceholder")}
          value={closeScenarioId}
          onChange={(e) => setCloseScenarioId(e.target.value)}
          className="h-9 w-40 rounded-md border border-input bg-background px-3 text-sm"
        />
        <select
          value={closeSeverity}
          onChange={(e) => setCloseSeverity(e.target.value as AlertSeverity | "")}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="">{t("alerts.bulkClose.severityPlaceholder")}</option>
          <option value="low">{t("alertSeverity.low")}</option>
          <option value="medium">{t("alertSeverity.medium")}</option>
          <option value="high">{t("alertSeverity.high")}</option>
          <option value="critical">{t("alertSeverity.critical")}</option>
        </select>
        <input
          type="text"
          placeholder={t("alerts.bulkClose.reasonPlaceholder")}
          value={closeReason}
          onChange={(e) => setCloseReason(e.target.value)}
          className="h-9 flex-1 min-w-[200px] rounded-md border border-input bg-background px-3 text-sm"
        />
        <Button
          size="sm"
          variant="destructive"
          disabled={busy || !closeReason.trim()}
          onClick={handleBulkClose}
        >
          {t("alerts.bulkClose.submit")}
        </Button>
      </div>

      {selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-muted/40 p-4">
          <span className="text-sm font-medium">{t("alerts.bulkCase.selectedCount", { count: selected.size })}</span>
          <Button size="sm" variant="outline" disabled={busy} onClick={handleBulkCase}>
            {t("alerts.bulkCase.submit")}
          </Button>
        </div>
      )}

      {actionError && <p className="text-sm text-destructive">{actionError}</p>}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10" />
              <TableHead>{t("alerts.table.header.severity")}</TableHead>
              <TableHead>{t("alerts.table.header.status")}</TableHead>
              <TableHead>{t("alerts.table.header.customerId")}</TableHead>
              <TableHead>{t("alerts.table.header.scenario")}</TableHead>
              <TableHead>{t("alerts.table.header.score")}</TableHead>
              <TableHead>{t("alerts.table.header.description")}</TableHead>
              <TableHead>{t("alerts.table.header.detectedAt")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {alerts && alerts.length > 0 ? (
              alerts.map((a) => (
                <TableRow key={a.id} className="cursor-pointer">
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      aria-label={`select-${a.id}`}
                      checked={selected.has(a.id)}
                      onChange={() => toggleSelected(a.id)}
                    />
                  </TableCell>
                  <TableCell>
                    <Link to={`/alerts/${a.id}`}>
                      <Badge variant={SEVERITY_VARIANT[a.severity]}>
                        {severityLabels[a.severity] ?? a.severity}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {statusLabels[a.status] ?? a.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-sm">{a.customer_id}</TableCell>
                  <TableCell className="font-mono text-sm">{a.scenario_id}</TableCell>
                  <TableCell>{a.score.toFixed(1)}</TableCell>
                  <TableCell className="max-w-[300px] truncate">{a.description}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(a.detected_at, i18n.language)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={8} className="h-24 text-center text-muted-foreground">
                  {t("alerts.table.empty")}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function TableSkeleton() {
  return (
    <div className="space-y-6">
      <div className="h-8 w-40 animate-pulse rounded bg-muted" />
      <div className="h-64 animate-pulse rounded-xl border bg-muted" />
    </div>
  )
}
