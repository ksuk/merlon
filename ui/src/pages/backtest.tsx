import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type BacktestJob, type BacktestResult, type Customer } from "@/lib/api"
import { FlaskConical, Play, RotateCcw, X } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

const pollingBackoffMs = [1000, 2000, 4000, 8000, 15000] as const
const maxPollingDurationMs = 10 * 60 * 1000

interface PollMessage {
  key: string
  detail?: string
}

function isAbortError(error: unknown) {
  return error instanceof Error && error.name === "AbortError"
}

export function BacktestPage() {
  const { t } = useTranslation()
  const [customerRefreshKey, setCustomerRefreshKey] = useState(0)
  const { data: page, loading, error } = useApi(() => api.customers.listAll(), customerRefreshKey)
  const { data: discoveredRulesPage, error: rulesError } = useApi(() => api.rules.listAll({ type: "TM_SCENARIO", activeOnly: true }))
  const [historyKey, setHistoryKey] = useState(0)
  const { data: jobHistory } = useApi(() => api.backtest.list({ limit: 20 }), historyKey)
  const customers = page?.data
  const discoveredRules = discoveredRulesPage?.data ?? []
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [running, setRunning] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [result, setResult] = useState<BacktestResult | null>(null)
  const [job, setJob] = useState<BacktestJob | null>(null)
  const { data: affectedCustomers } = useApi(
    () => job?.id && job.status === "completed" ? api.backtest.affectedCustomers(job.id) : Promise.resolve({ data: [], pagination: { has_more: false } }),
    job?.id && job.status === "completed" ? job.id : "no-completed-job",
  )
  const [pollMessage, setPollMessage] = useState<PollMessage | null>(null)
  const [from, setFrom] = useState(() => new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10))
  const [to, setTo] = useState(() => new Date().toISOString().slice(0, 10))
  const descRef = useRef<HTMLInputElement>(null)
  const [baselineRuleSet, setBaselineRuleSet] = useState("active")
  const [candidateRuleSet, setCandidateRuleSet] = useState("")
  const pollAbortRef = useRef<AbortController | null>(null)
  const pollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(false)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      pollAbortRef.current?.abort()
      if (pollTimerRef.current !== null) clearTimeout(pollTimerRef.current)
    }
  }, [])

  function toggleCustomer(id: string) {
    setSelectedIds((prev) => (prev.includes(id) ? prev.filter((i) => i !== id) : [...prev, id]))
  }

  function selectAll() {
    if (!customers) return
    setSelectedIds(selectedIds.length === customers.length ? [] : customers.map((c) => c.id))
  }

  function beginPollingSession() {
    pollAbortRef.current?.abort()
    if (pollTimerRef.current !== null) clearTimeout(pollTimerRef.current)
    const controller = new AbortController()
    pollAbortRef.current = controller
    setRunning(true)
    setPollMessage(null)
    return controller
  }

  function waitForNextPoll(delay: number, signal: AbortSignal) {
    return new Promise<void>((resolve, reject) => {
      const onAbort = () => {
        if (pollTimerRef.current !== null) clearTimeout(pollTimerRef.current)
        pollTimerRef.current = null
        reject(new DOMException("Polling aborted", "AbortError"))
      }
      pollTimerRef.current = setTimeout(() => {
        pollTimerRef.current = null
        signal.removeEventListener("abort", onAbort)
        resolve()
      }, delay)
      signal.addEventListener("abort", onAbort, { once: true })
    })
  }

  async function pollUntilTerminal(initial: BacktestJob, controller: AbortController) {
    let current = initial
    let attempt = 0
    const startedAt = Date.now()
    while (current.status === "queued" || current.status === "running") {
      const remaining = maxPollingDurationMs - (Date.now() - startedAt)
      if (remaining <= 0) throw new Error("POLLING_TIMEOUT")
      const delay = Math.min(pollingBackoffMs[Math.min(attempt, pollingBackoffMs.length - 1)], remaining)
      await waitForNextPoll(delay, controller.signal)
      if (Date.now() - startedAt >= maxPollingDurationMs) throw new Error("POLLING_TIMEOUT")
      current = await api.backtest.get(current.id, controller.signal)
      if (mountedRef.current && pollAbortRef.current === controller) setJob(current)
      attempt++
    }
    return current
  }

  async function runPolling(initial: BacktestJob, controller: AbortController) {
    try {
      const current = await pollUntilTerminal(initial, controller)
      if (!mountedRef.current || pollAbortRef.current !== controller) return
      setHistoryKey((key) => key + 1)
      if (current.status === "completed" && current.candidate) {
        setResult(current.candidate)
      } else if (current.status === "completed") {
        setPollMessage({ key: "backtest.polling.missingResult" })
      } else if (current.status === "failed") {
        setPollMessage({
          key: "backtest.polling.failed",
          detail: current.error,
        })
      } else if (current.status === "cancelled") {
        setPollMessage({ key: "backtest.polling.cancelled" })
      }
    } catch (error) {
      if (isAbortError(error) || !mountedRef.current || pollAbortRef.current !== controller) return
      if (error instanceof Error && error.message === "POLLING_TIMEOUT") {
        setPollMessage({ key: "backtest.polling.timeout" })
      } else {
        setPollMessage({
          key: "backtest.polling.error",
          detail: error instanceof Error ? error.message : String(error),
        })
      }
    } finally {
      if (mountedRef.current && pollAbortRef.current === controller) {
        pollAbortRef.current = null
        setRunning(false)
      }
    }
  }

  async function handleRun() {
    if (selectedIds.length === 0) return
    const candidate = candidateRuleSet.trim()
    if (!candidate) {
      setPollMessage({ key: "backtest.polling.candidateRequired" })
      return
    }
    if (candidate === baselineRuleSet) {
      setPollMessage({ key: "backtest.polling.rulesMustDiffer" })
      return
    }
    setResult(null)
    setJob(null)
    const controller = beginPollingSession()
    try {
      const created = await api.backtest.create(
        {
          from: `${from}T00:00:00Z`,
          to: `${to}T00:00:00Z`,
          customer_ids: selectedIds,
          scenario_ids: [],
          baseline_rule_set_id: baselineRuleSet,
          candidate_rule_set_id: candidate,
          rationale: descRef.current?.value.trim() || "",
        },
        controller.signal,
      )
      if (!mountedRef.current || pollAbortRef.current !== controller) return
      setJob(created)
      setHistoryKey((key) => key + 1)
      await runPolling(created, controller)
    } catch (error) {
      if (isAbortError(error) || !mountedRef.current || pollAbortRef.current !== controller) return
      setPollMessage({
        key: "backtest.polling.startError",
        detail: error instanceof Error ? error.message : String(error),
      })
      pollAbortRef.current = null
      setRunning(false)
    }
  }

  function handleResume() {
    if (!job || (job.status !== "queued" && job.status !== "running")) return
    void runPolling(job, beginPollingSession())
  }

  async function handleCancel() {
    if (!job || (job.status !== "queued" && job.status !== "running")) return
    pollAbortRef.current?.abort()
    pollAbortRef.current = null
    if (pollTimerRef.current !== null) clearTimeout(pollTimerRef.current)
    pollTimerRef.current = null
    setRunning(false)
    setCancelling(true)
    try {
      const cancelled = await api.backtest.cancel(job.id)
      if (!mountedRef.current) return
      setJob(cancelled)
      setPollMessage({ key: "backtest.polling.cancelled" })
    } catch (error) {
      if (!mountedRef.current) return
      setPollMessage({
        key: "backtest.polling.cancelError",
        detail: error instanceof Error ? error.message : String(error),
      })
    } finally {
      if (mountedRef.current) setCancelling(false)
    }
  }

  const hasActiveJob = job?.status === "queued" || job?.status === "running"
  const scenarioResults = result?.scenario_results ?? []

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error) {
    return <div role="alert" className="space-y-3 p-12 text-center text-destructive"><p>{t("backtest.error")}</p><Button variant="outline" onClick={() => setCustomerRefreshKey((key) => key + 1)}>{t("errorBoundary.retry")}</Button></div>
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("backtest.title")}</h1>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <FlaskConical className="h-4 w-4" />
            {t("backtest.form.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <label htmlFor="backtest-rationale" className="mb-1 block text-sm font-medium">{t("backtest.form.descriptionLabel")}</label>
            <input
              id="backtest-rationale"
              ref={descRef}
              placeholder={t("backtest.form.descriptionPlaceholder")}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <label className="text-sm font-medium">
              {t("backtest.form.from")}
              <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm" />
            </label>
            <label className="text-sm font-medium">
              {t("backtest.form.to")}
              <input type="date" value={to} onChange={(e) => setTo(e.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm" />
            </label>
          </div>
          <div>
              <label htmlFor="backtest-baseline" className="mb-1 block text-sm font-medium">
                {t("backtest.form.baselineRuleSet")}
              </label>
              <select id="backtest-baseline" value={baselineRuleSet} onChange={(event) => setBaselineRuleSet(event.target.value)} className="w-full rounded-md border bg-background px-3 py-2 text-sm">
                <option value="active">active</option>
                {(discoveredRules ?? []).map((rule) => <option key={`${rule.name}-${rule.version}`} value={rule.name}>{rule.name} v{rule.version}</option>)}
              </select>
              <label htmlFor="backtest-candidate" className="mb-1 mt-3 block text-sm font-medium">
              {t("backtest.form.candidateRuleSet")}
            </label>
            <input id="backtest-candidate" list="backtest-rule-discovery" value={candidateRuleSet} onChange={(event) => setCandidateRuleSet(event.target.value)} placeholder={t("backtest.form.candidatePlaceholder")} required className="w-full rounded-md border bg-background px-3 py-2 text-sm" />
            <datalist id="backtest-rule-discovery">
              {(discoveredRules ?? []).map((rule) => <option key={`${rule.name}-${rule.version}`} value={rule.name}>{rule.description ?? rule.name}</option>)}
            </datalist>
            {rulesError ? <p aria-live="polite" className="mt-1 text-xs text-destructive">{t("backtest.form.ruleDiscoveryError", { error: rulesError })}</p> : <p className="mt-1 text-xs text-muted-foreground">{t("backtest.form.ruleDiscovery", { count: discoveredRules?.length ?? 0 })}</p>}
          </div>
          <div>
            <div className="mb-2 flex items-center justify-between">
              <label className="text-sm font-medium">{t("backtest.form.targetCustomers")}</label>
              <Button variant="ghost" size="sm" onClick={selectAll}>
                {selectedIds.length === (customers?.length ?? 0) ? t("backtest.form.deselectAll") : t("backtest.form.selectAll")}
              </Button>
            </div>
            <div className="max-h-48 space-y-1 overflow-y-auto">
              {customers?.length === 0 ? (
                <p role="status" className="text-sm text-muted-foreground">{t("backtest.form.noCustomers")}</p>
              ) : customers?.map((c: Customer) => (
                    <button
                      key={c.id}
                      type="button"
                      aria-pressed={selectedIds.includes(c.id)}
                    onClick={() => toggleCustomer(c.id)}
                    className={`flex w-full items-center gap-3 rounded-md border p-2 text-left text-sm transition-colors ${selectedIds.includes(c.id) ? "border-primary bg-primary/5" : "hover:bg-accent"}`}
                  >
                    <div className={`h-3 w-3 rounded-sm border ${selectedIds.includes(c.id) ? "border-primary bg-primary" : "border-input"}`} />
                    <span className="font-mono text-xs">{c.external_id}</span>
                    <span className="text-muted-foreground">{c.country_code}</span>
                  </button>
                ))}
            </div>
            {customers && customers.length > 0 && <p className="text-xs text-muted-foreground">{t("list.allLoaded")}</p>}
          </div>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" disabled={running || cancelling || hasActiveJob || selectedIds.length === 0} onClick={handleRun}>
              <Play className="h-4 w-4" />
              {running ? t("backtest.form.running") : t("backtest.form.submit")}
            </Button>
            {hasActiveJob && (
              <Button size="sm" variant="outline" disabled={cancelling} onClick={handleCancel}>
                <X className="h-4 w-4" />
                {t("backtest.polling.cancel")}
              </Button>
            )}
            {!running && pollMessage && hasActiveJob && (
              <Button size="sm" variant="outline" onClick={handleResume}>
                <RotateCcw className="h-4 w-4" />
                {t("backtest.polling.resume")}
              </Button>
            )}
          </div>
          {job && (
            <div className="space-y-1 text-xs text-muted-foreground">
              {t("backtest.job.summary", {
                id: job.id,
                status: t(`backtest.job.status.${job.status}`),
                progress: Math.round(job.progress * 100),
              })}
              <p>{t("backtest.job.comparison", { baseline: job.baseline_rule_set_id, candidate: job.candidate_rule_set_id })}</p>
              {job.metadata?.rationale && <p>{t("backtest.job.rationale", { rationale: job.metadata.rationale })}</p>}
              {job.metadata?.cohort_preview && <div className="mt-2 rounded-md border p-2"><p className="font-medium text-foreground">{t("backtest.job.cohortPreview")}</p><p>{t("backtest.job.cohortCustomers", { count: Number(job.metadata.cohort_preview.count ?? selectedIds.length) })}</p><p>{t("backtest.job.cohortTransactions", { count: Number(job.metadata.cohort_preview.transaction_count ?? 0) })}</p>{Number(job.metadata.cohort_preview.transaction_count ?? 0) === 0 && <p role="alert" className="text-destructive">{t("backtest.job.emptyCohort")}</p>}</div>}
            </div>
          )}
          {pollMessage && (
            <p role="alert" className="text-sm text-destructive">
              {t(pollMessage.key)}
              {pollMessage.detail ? `: ${pollMessage.detail}` : ""}
            </p>
          )}
        </CardContent>
      </Card>

      {job && (job.baseline || job.candidate || job.delta) && <Card><CardHeader><CardTitle className="text-base">{t("backtest.job.sideBySide")}</CardTitle></CardHeader><CardContent className="grid gap-3 md:grid-cols-3"><ComparisonColumn label={t("backtest.job.baseline")} result={job.baseline} /><ComparisonColumn label={t("backtest.job.candidate")} result={job.candidate} /><ComparisonColumn label={t("backtest.job.delta")} result={job.delta} /></CardContent></Card>}

      {job && job.status === "completed" && <Card><CardHeader><CardTitle className="text-base">{t("backtest.job.affectedCustomers")}</CardTitle></CardHeader><CardContent>{affectedCustomers?.data.length ? <p className="font-mono text-xs">{affectedCustomers.data.join(", ")}</p> : <p className="text-sm text-muted-foreground">{t("backtest.job.noAffectedCustomers")}</p>}</CardContent></Card>}

      <Card><CardHeader><CardTitle className="text-base">{t("backtest.job.history")}</CardTitle></CardHeader><CardContent>{jobHistory?.data.length ? <Table><TableHeader><TableRow><TableHead>{t("backtest.job.historyId")}</TableHead><TableHead>{t("backtest.job.historyStatus")}</TableHead><TableHead>{t("backtest.job.historyComparison")}</TableHead></TableRow></TableHeader><TableBody>{jobHistory.data.map((item) => <TableRow key={item.id}><TableCell className="font-mono text-xs">{item.id}</TableCell><TableCell><Badge variant={item.status === "failed" ? "critical" : "outline"}>{t(`backtest.job.status.${item.status}`)}</Badge></TableCell><TableCell className="text-xs">{item.baseline_rule_set_id} → {item.candidate_rule_set_id}{item.metadata?.rerun_of ? ` (${item.metadata.rerun_of})` : ""}</TableCell></TableRow>)}</TableBody></Table> : <p className="text-sm text-muted-foreground">{t("backtest.job.historyEmpty")}</p>}</CardContent></Card>

      {result && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("backtest.result.title")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-4 gap-4">
              <div className="rounded-md border p-3 text-center">
                <p className="text-2xl font-bold">{result.total_customers}</p>
                <p className="text-xs text-muted-foreground">{t("backtest.result.customers")}</p>
              </div>
              <div className="rounded-md border p-3 text-center">
                <p className="text-2xl font-bold">{result.total_transactions}</p>
                <p className="text-xs text-muted-foreground">{t("backtest.result.transactions")}</p>
              </div>
              <div className="rounded-md border p-3 text-center">
                <p className="text-2xl font-bold">{result.total_alerts}</p>
                <p className="text-xs text-muted-foreground">{t("backtest.result.alerts")}</p>
              </div>
              <div className="rounded-md border p-3 text-center">
                <p className="text-2xl font-bold">{result.execution_time_ms.toFixed(0)}ms</p>
                <p className="text-xs text-muted-foreground">{t("backtest.result.executionTime")}</p>
              </div>
            </div>

            {scenarioResults.length === 0 ? (
              <p role="status" className="text-sm text-muted-foreground">
                {t("backtest.result.empty")}
              </p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("backtest.result.table.header.scenario")}</TableHead>
                    <TableHead className="text-right">{t("backtest.result.table.header.alerts")}</TableHead>
                    <TableHead className="text-right">{t("backtest.result.table.header.high")}</TableHead>
                    <TableHead className="text-right">{t("backtest.result.table.header.medium")}</TableHead>
                    <TableHead className="text-right">{t("backtest.result.table.header.low")}</TableHead>
                    <TableHead className="text-right">{t("backtest.result.table.header.affectedCustomers")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {scenarioResults.map((s) => (
                    <TableRow key={s.scenario_id}>
                      <TableCell className="font-mono text-xs">{s.scenario_id}</TableCell>
                      <TableCell className="text-right">{s.alerts_generated}</TableCell>
                      <TableCell className="text-right">
                        <Badge variant="high">{s.high_severity_count}</Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <Badge variant="medium">{s.medium_severity_count}</Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <Badge variant="low">{s.low_severity_count}</Badge>
                      </TableCell>
                      <TableCell className="text-right">{s.affected_customer_ids.length}</TableCell>
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

function ComparisonColumn({ label, result }: { label: string; result?: BacktestResult }) {
  return <div className="rounded-md border p-3"><h3 className="text-sm font-semibold">{label}</h3><dl className="mt-2 space-y-1 text-sm"><div className="flex justify-between"><dt className="text-muted-foreground">Customers</dt><dd>{result?.total_customers ?? "-"}</dd></div><div className="flex justify-between"><dt className="text-muted-foreground">Transactions</dt><dd>{result?.total_transactions ?? "-"}</dd></div><div className="flex justify-between"><dt className="text-muted-foreground">Alerts</dt><dd>{result?.total_alerts ?? "-"}</dd></div></dl></div>
}
