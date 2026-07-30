import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type BatchMonitorResponse, type BatchScoreResponse, type Customer } from "@/lib/api"
import { Play, RefreshCw, Shield } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

export function BatchPage() {
  const { t } = useTranslation()
  const { data: page, loading, error } = useApi(api.customers.list)
  const customers = page?.data
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [scoreRunning, setScoreRunning] = useState(false)
  const [monitorRunning, setMonitorRunning] = useState(false)
  const [scoreResult, setScoreResult] = useState<BatchScoreResponse | null>(null)
  const [monitorResult, setMonitorResult] = useState<BatchMonitorResponse | null>(null)

  function toggleCustomer(id: string) {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((i) => i !== id) : [...prev, id],
    )
  }

  function selectAll() {
    if (!customers) return
    setSelectedIds(
      selectedIds.length === customers.length ? [] : customers.map((c) => c.id),
    )
  }

  async function handleBatchScore() {
    setScoreRunning(true)
    setScoreResult(null)
    try {
      const ids = selectedIds.length > 0 ? selectedIds : undefined
      const res = await api.batch.score(ids)
      setScoreResult(res)
    } finally {
      setScoreRunning(false)
    }
  }

  async function handleBatchMonitor() {
    setMonitorRunning(true)
    setMonitorResult(null)
    try {
      const ids = selectedIds.length > 0 ? selectedIds : undefined
      const res = await api.batch.monitor(ids)
      setMonitorResult(res)
    } finally {
      setMonitorRunning(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">{t("batch.error")}</p>
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("batch.title")}</h1>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("batch.targetCustomers.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              {selectedIds.length > 0
                ? t("batch.targetCustomers.selectedCount", { count: selectedIds.length })
                : t("batch.targetCustomers.allTargeted")}
            </p>
            <Button variant="ghost" size="sm" onClick={selectAll}>
              {selectedIds.length === (customers?.length ?? 0) ? t("batch.targetCustomers.deselectAll") : t("batch.targetCustomers.selectAll")}
            </Button>
          </div>
          <div className="max-h-48 space-y-1 overflow-y-auto">
            {customers?.map((c: Customer) => (
              <button key={c.id} type="button" onClick={() => toggleCustomer(c.id)}
                className={`flex w-full items-center gap-3 rounded-md border p-2 text-left text-sm transition-colors ${selectedIds.includes(c.id) ? "border-primary bg-primary/5" : "hover:bg-accent"}`}>
                <div className={`h-3 w-3 rounded-sm border ${selectedIds.includes(c.id) ? "border-primary bg-primary" : "border-input"}`} />
                <span className="font-mono text-xs">{c.external_id}</span>
                <span className="text-muted-foreground">{c.country_code}</span>
              </button>
            ))}
          </div>
          <div className="flex gap-2">
            <Button size="sm" onClick={handleBatchScore} disabled={scoreRunning}>
              <RefreshCw className={`h-4 w-4 ${scoreRunning ? "animate-spin" : ""}`} />
              {t("batch.actions.score")}
            </Button>
            <Button size="sm" variant="outline" onClick={handleBatchMonitor} disabled={monitorRunning}>
              <Shield className={`h-4 w-4 ${monitorRunning ? "animate-pulse" : ""}`} />
              {t("batch.actions.monitor")}
            </Button>
          </div>
        </CardContent>
      </Card>

      {scoreResult && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Play className="h-4 w-4" />
              {t("batch.scoreResult.title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-4 gap-4">
              <Stat label={t("batch.scoreResult.stats.total")} value={scoreResult.total} />
              <Stat label={t("batch.scoreResult.stats.succeeded")} value={scoreResult.succeeded} />
              <Stat label={t("batch.scoreResult.stats.failed")} value={scoreResult.failed} />
              <Stat label={t("batch.scoreResult.stats.duration")} value={scoreResult.duration} />
            </div>
            {scoreResult.results.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("batch.scoreResult.table.header.customerId")}</TableHead>
                    <TableHead>{t("batch.scoreResult.table.header.score")}</TableHead>
                    <TableHead>{t("batch.scoreResult.table.header.riskTier")}</TableHead>
                    <TableHead>{t("batch.scoreResult.table.header.error")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {scoreResult.results.map((r) => (
                    <TableRow key={r.customer_id}>
                      <TableCell className="font-mono text-xs">{r.customer_id}</TableCell>
                      <TableCell>{r.error ? "-" : r.score.toFixed(1)}</TableCell>
                      <TableCell>
                        {r.risk_tier ? <Badge variant="outline">{r.risk_tier}</Badge> : "-"}
                      </TableCell>
                      <TableCell className="text-xs text-destructive">{r.error || ""}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}

      {monitorResult && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Shield className="h-4 w-4" />
              {t("batch.monitorResult.title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-4 gap-4">
              <Stat label={t("batch.monitorResult.stats.total")} value={monitorResult.total} />
              <Stat label={t("batch.monitorResult.stats.succeeded")} value={monitorResult.succeeded} />
              <Stat label={t("batch.monitorResult.stats.alertsTotal")} value={monitorResult.alerts_total} />
              <Stat label={t("batch.monitorResult.stats.duration")} value={monitorResult.duration} />
            </div>
            {monitorResult.results.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("batch.monitorResult.table.header.customerId")}</TableHead>
                    <TableHead>{t("batch.monitorResult.table.header.alertsRaised")}</TableHead>
                    <TableHead>{t("batch.monitorResult.table.header.error")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {monitorResult.results.map((r) => (
                    <TableRow key={r.customer_id}>
                      <TableCell className="font-mono text-xs">{r.customer_id}</TableCell>
                      <TableCell>
                        {r.alerts_raised > 0 ? (
                          <Badge variant="high">{r.alerts_raised}</Badge>
                        ) : "0"}
                      </TableCell>
                      <TableCell className="text-xs text-destructive">{r.error || ""}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-md border p-3 text-center">
      <p className="text-2xl font-bold">{value}</p>
      <p className="text-xs text-muted-foreground">{label}</p>
    </div>
  )
}
