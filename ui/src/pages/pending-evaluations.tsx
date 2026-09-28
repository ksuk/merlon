import { CursorPager, useCursorPager } from "@/components/cursor-pager"
import { formatDuration } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type PendingEvaluation } from "@/lib/api"
import { RefreshCw, RotateCcw } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useSearchParams } from "react-router"

const statusOptions = ["", "PENDING_REVIEW", "PROCESSING", "FAILED", "RESOLVED"] as const

export function PendingEvaluationsPage() {
  const { t, i18n } = useTranslation()
  const [searchParams] = useSearchParams()
  const pendingEvaluationID = searchParams.get("pending_evaluation_id") ?? ""
  // The queue's stop conditions. A page of rows cannot answer "how much is
  // outstanding" or "how long has the oldest gap been open"; approximating
  // them from loaded rows reads as a small backlog exactly when it is large.
  const { data: stats } = useApi(() => api.pending.stats())
  const [status, setStatus] = useState("")
  const [customerFilter, setCustomerFilter] = useState("")
  const [batchRunFilter, setBatchRunFilter] = useState("")
  const [createdFrom, setCreatedFrom] = useState("")
  const [createdTo, setCreatedTo] = useState("")
  const [minAgeDays, setMinAgeDays] = useState("")
  const [maxAgeDays, setMaxAgeDays] = useState("")
  const [selectedOverride, setSelected] = useState<PendingEvaluation | null | undefined>(undefined)
  const [reason, setReason] = useState("")
  const [refreshKey, setRefreshKey] = useState(0)
  const [actionError, setActionError] = useState<string | null>(null)
  const [acting, setActing] = useState(false)
  const minAge = minAgeDays.trim() === "" ? undefined : Number(minAgeDays)
  const maxAge = maxAgeDays.trim() === "" ? undefined : Number(maxAgeDays)
  const filterKey = [status, customerFilter, batchRunFilter, createdFrom, createdTo, minAgeDays, maxAgeDays, refreshKey].join(":")
  const pager = useCursorPager(filterKey)
  const { data: page, loading, error } = useApi(() => api.pending.list({
    status: status || undefined,
    customerId: customerFilter.trim() || undefined,
    batchRunId: batchRunFilter.trim() || undefined,
    createdFrom: createdFrom ? `${createdFrom}T00:00:00Z` : undefined,
    createdTo: createdTo ? `${createdTo}T23:59:59.999Z` : undefined,
    minAgeDays: Number.isFinite(minAge) ? minAge : undefined,
    maxAgeDays: Number.isFinite(maxAge) ? maxAge : undefined,
    limit: 50,
    cursor: pager.cursor || undefined,
  }), pager.requestKey)
  const { data: deepLinkedEvaluation, error: deepLinkError } = useApi(
    () => pendingEvaluationID ? api.pending.get(pendingEvaluationID) : Promise.resolve(null),
    `${pendingEvaluationID}:${refreshKey}`,
  )
  const selected = selectedOverride === undefined ? deepLinkedEvaluation : selectedOverride
  const { data: history } = useApi(() => selected ? api.pending.history(selected.id) : Promise.resolve([]), `${selected?.id ?? ""}:${selected?.version ?? 0}`)

  async function transition(action: "retry" | "resolve" | "escalate") {
    if (!selected) return
    if (!reason.trim()) {
      setActionError(t("pendingEvaluations.rationaleRequired"))
      return
    }
    setActing(true)
    setActionError(null)
    try {
      const updated = await api.pending.transition(selected.id, action, { reason: reason.trim(), expected_version: selected.version })
      setSelected(updated)
      setReason("")
      setRefreshKey((key) => key + 1)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setActing(false)
    }
  }

  if (loading) return <div className="space-y-6"><div className="h-8 w-64 animate-pulse rounded bg-muted" /><div className="h-64 animate-pulse rounded-xl border bg-muted" /></div>
  if (error) return <div className="space-y-3 p-12 text-center"><p role="alert" className="text-destructive">{t("pendingEvaluations.error")}: {error}</p><Button variant="outline" onClick={() => setRefreshKey((key) => key + 1)}><RefreshCw className="h-4 w-4" />{t("pendingEvaluations.retry")}</Button></div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between"><div><h1 className="text-2xl font-bold tracking-tight">{t("pendingEvaluations.title")}</h1><p className="text-sm text-muted-foreground">{t("pendingEvaluations.description")}</p></div><Button size="sm" variant="outline" onClick={() => setRefreshKey((key) => key + 1)}><RefreshCw className="h-4 w-4" />{t("pendingEvaluations.refresh")}</Button></div>
      {deepLinkError && <p role="alert" className="rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">{t("pendingEvaluations.deepLinkError")}: {deepLinkError}</p>}
      {actionError && <p role="alert" className="rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">{actionError}</p>}
      {stats && (
        <Card data-testid="pending-stats">
          <CardHeader><CardTitle className="text-base">{t("pendingEvaluations.stopCondition")}</CardTitle></CardHeader>
          <CardContent>
            <dl className="grid gap-3 text-sm sm:grid-cols-4">
              <div>
                <dt className="text-muted-foreground">{t("pendingEvaluations.backlog")}</dt>
                <dd data-testid="pending-backlog" className="text-lg font-semibold">{stats.backlog}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t("pendingEvaluations.oldest")}</dt>
                <dd data-testid="pending-oldest" className="text-lg font-semibold">
                  {stats.oldest_created_at ? formatDuration(stats.oldest_age_seconds, t) : "-"}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t("pendingEvaluations.failedCount")}</dt>
                <dd className="text-lg font-semibold">{stats.failed}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t("pendingEvaluations.exhaustedCount")}</dt>
                <dd data-testid="pending-exhausted" className="text-lg font-semibold">{stats.exhausted}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      )}
      <Card>
        <CardHeader className="space-y-3"><div className="flex-row items-center justify-between"><CardTitle className="flex items-center gap-2 text-base"><RotateCcw className="h-4 w-4" />{t("pendingEvaluations.queue")}</CardTitle><label htmlFor="pending-status-filter" className="flex items-center gap-2 text-sm"><span>{t("pendingEvaluations.filter")}</span><select id="pending-status-filter" value={status} onChange={(event) => setStatus(event.target.value)} className="rounded-md border bg-background px-2 py-1">{statusOptions.map((item) => <option key={item || "all"} value={item}>{item ? item : t("pendingEvaluations.all")}</option>)}</select></label></div><div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-6"><FilterInput id="pending-customer-filter" label={t("pendingEvaluations.customerFilter")} value={customerFilter} onChange={setCustomerFilter} /><FilterInput id="pending-run-filter" label={t("pendingEvaluations.runFilter")} value={batchRunFilter} onChange={setBatchRunFilter} /><FilterInput id="pending-created-from" label={t("pendingEvaluations.createdFrom")} type="date" value={createdFrom} onChange={setCreatedFrom} /><FilterInput id="pending-created-to" label={t("pendingEvaluations.createdTo")} type="date" value={createdTo} onChange={setCreatedTo} /><FilterInput id="pending-min-age" label={t("pendingEvaluations.minAgeDays")} type="number" min="0" value={minAgeDays} onChange={setMinAgeDays} /><FilterInput id="pending-max-age" label={t("pendingEvaluations.maxAgeDays")} type="number" min="0" value={maxAgeDays} onChange={setMaxAgeDays} /></div></CardHeader>
        <CardContent>{page?.data.length === 0 ? <p role="status" className="py-8 text-center text-sm text-muted-foreground">{t("pendingEvaluations.empty")}</p> : <div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>{t("pendingEvaluations.customer")}</TableHead><TableHead>{t("pendingEvaluations.statusLabel")}</TableHead><TableHead>{t("pendingEvaluations.reason")}</TableHead><TableHead>{t("pendingEvaluations.retryCount")}</TableHead><TableHead>{t("pendingEvaluations.createdAt")}</TableHead></TableRow></TableHeader><TableBody>{page?.data.map((item) => <TableRow key={item.id} role="button" tabIndex={0} aria-selected={selected?.id === item.id} aria-label={`${t("pendingEvaluations.detail")}: ${item.customer_id}`} className="cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => setSelected(item)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); setSelected(item) } }}><TableCell><Link className="text-primary hover:underline" to={`/customers/${item.customer_id}`} onClick={(event) => event.stopPropagation()}>{item.customer_id}</Link><div className="text-xs text-muted-foreground">{item.transaction_ids.length} {t("pendingEvaluations.transactions")}</div></TableCell><TableCell><Badge variant={item.status === "FAILED" ? "critical" : item.status === "RESOLVED" ? "secondary" : "outline"}>{item.status}</Badge></TableCell><TableCell className="max-w-xs truncate">{item.reason}</TableCell><TableCell>{item.retry_count}</TableCell><TableCell className="whitespace-nowrap text-xs">{new Date(item.created_at).toLocaleString(i18n.language)}</TableCell></TableRow>)}</TableBody></Table></div>}<CursorPager pager={pager} nextCursor={page?.pagination?.next_cursor} testId="pending-pager" /></CardContent>
      </Card>
      {selected && <Card><CardHeader className="flex-row items-center justify-between"><CardTitle className="text-base">{t("pendingEvaluations.detail")}</CardTitle><Button variant="ghost" size="sm" onClick={() => setSelected(null)}>{t("pendingEvaluations.close")}</Button></CardHeader><CardContent className="space-y-4"><dl className="grid gap-2 text-sm sm:grid-cols-2"><div><dt className="text-muted-foreground">{t("pendingEvaluations.statusLabel")}</dt><dd><Badge>{selected.status}</Badge></dd></div><div><dt className="text-muted-foreground">{t("pendingEvaluations.nextRetry")}</dt><dd>{selected.next_retry_at ? new Date(selected.next_retry_at).toLocaleString(i18n.language) : "-"}</dd></div><div className="sm:col-span-2"><dt className="text-muted-foreground">{t("pendingEvaluations.reason")}</dt><dd>{selected.reason}</dd></div><div><dt className="text-muted-foreground">{t("pendingEvaluations.alerts")}</dt><dd>{selected.alert_ids?.length ? selected.alert_ids.map((alertID) => <Link key={alertID} className="mr-2 text-primary hover:underline" to={`/alerts/${alertID}`}>{alertID}</Link>) : "-"}</dd></div><div><dt className="text-muted-foreground">{t("pendingEvaluations.batchRun")}</dt><dd>{selected.batch_run_id ? <Link data-testid="pending-batch-run-link" className="text-primary hover:underline" to={`/batch?run=${encodeURIComponent(selected.batch_run_id)}`}>{selected.batch_run_id}</Link> : "-"}</dd></div></dl><div><label htmlFor="pending-action-reason" className="mb-1 block text-sm font-medium">{t("pendingEvaluations.actionReason")}</label><textarea id="pending-action-reason" value={reason} onChange={(event) => setReason(event.target.value)} className="min-h-20 w-full rounded-md border bg-background px-3 py-2 text-sm" placeholder={t("pendingEvaluations.actionReasonPlaceholder")} /></div><div className="flex flex-wrap gap-2"><Button size="sm" disabled={acting || selected.status === "RESOLVED"} onClick={() => transition("retry")}>{t("pendingEvaluations.retryAction")}</Button><Button size="sm" variant="outline" disabled={acting || selected.status === "RESOLVED"} onClick={() => transition("resolve")}>{t("pendingEvaluations.resolveAction")}</Button><Button size="sm" variant="destructive" disabled={acting || selected.status === "RESOLVED"} onClick={() => transition("escalate")}>{t("pendingEvaluations.escalateAction")}</Button></div>{history && history.length > 0 && <div><h3 className="mb-2 text-sm font-medium">{t("pendingEvaluations.history")}</h3><ul className="space-y-1 text-xs text-muted-foreground">{history.map((entry) => <li key={entry.id}>{entry.action}: {entry.from_status} → {entry.to_status} — {entry.reason}</li>)}</ul></div>}</CardContent></Card>}
    </div>
  )
}

function FilterInput({ id, label, value, onChange, type = "text", min }: { id: string; label: string; value: string; onChange: (value: string) => void; type?: string; min?: string }) {
  return <label htmlFor={id} className="text-xs font-medium"><span className="mb-1 block">{label}</span><input id={id} type={type} min={min} value={value} onChange={(event) => onChange(event.target.value)} className="w-full rounded-md border bg-background px-2 py-1 text-sm" /></label>
}
