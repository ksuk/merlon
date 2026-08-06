import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type BatchRun, type Customer, type TargetManifest } from "@/lib/api"
import { Play, RefreshCw, Shield } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

type Operation = "batch_score" | "batch_monitor"
type TargetMode = "selected" | "all"

export function BatchPage() {
  const { t } = useTranslation()
  const { data: page, loading, error } = useApi(api.customers.listAll)
  const customers = page?.data ?? []
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [targetMode, setTargetMode] = useState<TargetMode>("selected")
  const [preview, setPreview] = useState<TargetManifest | null>(null)
  const [run, setRun] = useState<BatchRun | null>(null)
  const [operation, setOperation] = useState<Operation>("batch_score")
  const [rationale, setRationale] = useState("")
  const [busy, setBusy] = useState(false)
  const [batchError, setBatchError] = useState<string | null>(null)
  const [runsKey, setRunsKey] = useState(0)
  const { data: runsPage } = useApi(() => api.batch.runs({ limit: 20 }), runsKey)

  function toggleCustomer(id: string) {
    setSelectedIds((prev) => prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id])
    setPreview(null)
  }

  function selectAll() {
    setSelectedIds((current) => current.length === customers.length ? [] : customers.map((customer) => customer.id))
    setPreview(null)
  }

  async function handlePreview(nextOperation: Operation) {
    if (targetMode === "selected" && selectedIds.length === 0) {
      setBatchError(t("batch.targetCustomers.selectionRequired"))
      return
    }
    setOperation(nextOperation)
    setBusy(true)
    setBatchError(null)
    try {
      const manifest = await api.batch.preview({
        operation: nextOperation,
        target_mode: targetMode,
        customer_ids: targetMode === "selected" ? selectedIds : undefined,
        rationale: rationale.trim() || undefined,
      })
      setPreview(manifest)
      setRun(null)
    } catch (err) {
      setBatchError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleConfirm() {
    if (!preview) return
    if (!rationale.trim()) {
      setBatchError(t("batch.confirmation.rationaleRequired"))
      return
    }
    setBusy(true)
    setBatchError(null)
    try {
      const confirmed = await api.batch.confirm(preview.id, preview.token ?? "", preview.version, rationale.trim())
      const created = await api.batch.createRun({
        operation,
        target_manifest_id: confirmed.id,
        parameters: { target_mode: targetMode },
        rationale: rationale.trim(),
        idempotency_key: `${operation}:${confirmed.id}`,
      })
      setRun(created)
      setPreview(null)
      setRunsKey((key) => key + 1)
    } catch (err) {
      setBatchError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <div className="space-y-6"><div className="h-8 w-48 animate-pulse rounded bg-muted" /><div className="h-64 animate-pulse rounded-xl border bg-muted" /></div>
  if (error) return <div className="space-y-3 p-12 text-center"><p role="alert" className="text-destructive"><span>{t("batch.error")}</span>: {error}</p><Button variant="outline" onClick={() => window.location.reload()}>{t("batch.retry")}</Button></div>

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("batch.title")}</h1>
      {batchError && <p role="alert" className="text-sm text-destructive">{t("batch.operationError", { error: batchError })}</p>}
      <Card>
        <CardHeader><CardTitle className="text-base">{t("batch.targetCustomers.title")}</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <fieldset className="space-y-2"><legend className="text-sm font-medium">{t("batch.targetCustomers.mode")}</legend><div className="flex flex-wrap gap-4 text-sm"><label className="flex items-center gap-2"><input type="radio" name="batch-target-mode" value="selected" checked={targetMode === "selected"} onChange={() => { setTargetMode("selected"); setPreview(null) }} />{t("batch.targetCustomers.selectedMode")}</label><label className="flex items-center gap-2"><input type="radio" name="batch-target-mode" value="all" checked={targetMode === "all"} onChange={() => { setTargetMode("all"); setPreview(null) }} />{t("batch.targetCustomers.allMode")}</label></div></fieldset>
          <div className="flex items-center justify-between"><p className="text-sm text-muted-foreground">{targetMode === "all" ? t("batch.targetCustomers.explicitAll") : t("batch.targetCustomers.selectedCount", { count: selectedIds.length })}</p><Button variant="ghost" size="sm" onClick={selectAll} disabled={targetMode === "all"}>{selectedIds.length === customers.length ? t("batch.targetCustomers.deselectAll") : t("batch.targetCustomers.selectAll")}</Button></div>
          <div className="max-h-48 space-y-1 overflow-y-auto">{customers.length === 0 ? <p role="status" className="text-sm text-muted-foreground">{t("batch.targetCustomers.noCustomers")}</p> : customers.map((customer: Customer) => <button key={customer.id} type="button" onClick={() => toggleCustomer(customer.id)} aria-pressed={selectedIds.includes(customer.id)} disabled={targetMode === "all"} className={`flex w-full items-center gap-3 rounded-md border p-2 text-left text-sm transition-colors ${selectedIds.includes(customer.id) ? "border-primary bg-primary/5" : "hover:bg-accent"} disabled:cursor-not-allowed disabled:opacity-60`}><span aria-hidden="true" className={`h-3 w-3 rounded-sm border ${selectedIds.includes(customer.id) ? "border-primary bg-primary" : "border-input"}`} /><span className="font-mono text-xs">{customer.external_id}</span><span className="text-muted-foreground">{customer.country_code}</span></button>)}</div>
          {customers.length > 0 && <p className="text-xs text-muted-foreground">{t("list.allLoaded")}</p>}
          <div><label htmlFor="batch-rationale" className="mb-1 block text-sm font-medium">{t("batch.confirmation.rationale")}</label><textarea id="batch-rationale" value={rationale} onChange={(event) => { setRationale(event.target.value); setPreview(null) }} className="min-h-20 w-full rounded-md border bg-background px-3 py-2 text-sm" placeholder={t("batch.confirmation.rationalePlaceholder")} /></div>
          <div className="flex gap-2"><Button size="sm" onClick={() => void handlePreview("batch_score")} disabled={busy}><RefreshCw className={`h-4 w-4 ${busy && operation === "batch_score" ? "animate-spin" : ""}`} />{t("batch.actions.score")}</Button><Button size="sm" variant="outline" onClick={() => void handlePreview("batch_monitor")} disabled={busy}><Shield className={`h-4 w-4 ${busy && operation === "batch_monitor" ? "animate-pulse" : ""}`} />{t("batch.actions.monitor")}</Button></div>
        </CardContent>
      </Card>
      {preview && <Card className="border-primary"><CardHeader><CardTitle className="text-base">{t("batch.confirmation.title")}</CardTitle></CardHeader><CardContent className="space-y-3"><p className="text-sm">{t("batch.confirmation.summary", { count: preview.target_count, operation: operation === "batch_score" ? t("batch.actions.score") : t("batch.actions.monitor") })}</p><dl className="grid gap-2 text-sm sm:grid-cols-3"><div><dt className="text-muted-foreground">{t("batch.confirmation.mode")}</dt><dd>{preview.target_mode}</dd></div><div><dt className="text-muted-foreground">{t("batch.confirmation.criteria")}</dt><dd>{preview.criteria || "-"}</dd></div><div><dt className="text-muted-foreground">{t("batch.confirmation.expires")}</dt><dd>{new Date(preview.expires_at).toLocaleString()}</dd></div></dl><p role="status" aria-live="polite" className="text-xs text-muted-foreground">{t("batch.confirmation.warning")}</p><Button onClick={() => void handleConfirm()} disabled={busy}>{t("batch.confirmation.confirm")}</Button></CardContent></Card>}
      {run && <Card><CardHeader><CardTitle className="flex items-center gap-2 text-base"><Play className="h-4 w-4" />{t("batch.run.title")}</CardTitle></CardHeader><CardContent className="space-y-2"><p role="status" aria-live="polite">{t("batch.run.status", { id: run.id, status: run.status })}</p>{(run.result_counts?.queued_for_review ?? 0) > 0 && <div role="alert" className="rounded-md border border-orange-300 bg-orange-50 p-3 text-sm text-orange-900"><div className="flex items-center gap-2 font-semibold"><span>{t("batch.monitorResult.reviewRequired.title")}</span><Badge variant="critical">{t("batch.monitorResult.reviewRequired.badge")}</Badge></div><p>{t("batch.monitorResult.reviewRequired.description", { count: run.result_counts.queued_for_review })}</p><Badge variant="critical">{t("batch.monitorResult.row.pendingReview")}</Badge></div>}<p className="text-sm text-muted-foreground">{Object.entries(run.result_counts ?? {}).map(([key, value]) => `${key}: ${value}`).join(" · ") || t("batch.run.noCounts")}</p>{run.customer_outcomes && Object.keys(run.customer_outcomes).length > 0 && <div className="overflow-x-auto"><h3 className="mb-2 text-sm font-medium">{t("batch.run.customerOutcomes")}</h3><Table><TableHeader><TableRow><TableHead>{t("batch.run.customer")}</TableHead><TableHead>{t("batch.run.outcome")}</TableHead><TableHead>{t("batch.run.alerts")}</TableHead><TableHead>{t("batch.run.errorLabel")}</TableHead></TableRow></TableHeader><TableBody>{Object.values(run.customer_outcomes).map((outcome) => <TableRow key={outcome.customer_id}><TableCell><Link className="text-primary hover:underline" to={`/customers/${outcome.customer_id}`}>{outcome.customer_id}</Link></TableCell><TableCell><Badge variant={outcome.status === "failed" || outcome.status === "error" ? "critical" : outcome.status === "succeeded" ? "low" : "outline"}>{outcome.status}</Badge></TableCell><TableCell>{outcome.alert_ids?.length ?? 0}</TableCell><TableCell>{outcome.error ?? "-"}</TableCell></TableRow>)}</TableBody></Table></div>}{run.error && <p role="alert" className="text-sm text-destructive">{run.error}</p>}</CardContent></Card>}
      <Card><CardHeader><CardTitle className="text-base">{t("batch.run.history")}</CardTitle></CardHeader><CardContent>{runsPage?.data.length ? <Table><TableHeader><TableRow><TableHead>{t("batch.run.id")}</TableHead><TableHead>{t("batch.run.operation")}</TableHead><TableHead>{t("batch.run.statusLabel")}</TableHead><TableHead>{t("batch.run.startedAt")}</TableHead></TableRow></TableHeader><TableBody>{runsPage.data.map((item) => <TableRow key={item.id}><TableCell className="font-mono text-xs">{item.id}</TableCell><TableCell>{item.operation}</TableCell><TableCell><Badge variant={item.status === "failed" || item.status === "partial" ? "critical" : "outline"}>{item.status}</Badge></TableCell><TableCell className="text-xs">{new Date(item.started_at).toLocaleString()}</TableCell></TableRow>)}</TableBody></Table> : <p role="status" className="text-sm text-muted-foreground">{t("batch.run.empty")}</p>}</CardContent></Card>
    </div>
  )
}
