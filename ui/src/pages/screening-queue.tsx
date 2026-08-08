import { formatDuration } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { usePolicy } from "@/hooks/use-policy"
import { api, type ScreeningResultRecord, type ScreeningResultStatus } from "@/lib/api"
import { RefreshCw, ShieldAlert } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

const statuses: Array<ScreeningResultStatus | ""> = ["", "NEW", "REVIEWING", "TRUE_POSITIVE", "FALSE_POSITIVE"]

export function ScreeningQueuePage() {
  const { t, i18n } = useTranslation()
  const [status, setStatus] = useState<ScreeningResultStatus | "">("")
  const [showSuppressed, setShowSuppressed] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [acting, setActing] = useState<string | null>(null)
  const [rationale, setRationale] = useState<Record<string, string>>({})
  const [actionError, setActionError] = useState<string | null>(null)
  const { data: page, loading, error } = useApi(
    // Suppressed hits are repeat false positives an operator already decided
    // on; they are hidden by default and shown on request so what was hidden
    // stays auditable.
    () => api.screening.results({ status: status || undefined, suppressed: showSuppressed ? undefined : false, limit: 50 }),
    `${status}:${showSuppressed}:${refreshKey}`,
  )
  const { data: sources, error: sourcesError } = useApi(() => api.screening.sources(), refreshKey)
  const { data: readiness } = usePolicy("screening_readiness")
  const requiredSources = new Set((readiness?.document?.sources ?? []).filter((source) => source.required).map((source) => source.list_id))

  async function review(result: ScreeningResultRecord, next: ScreeningResultStatus) {
    const reason = rationale[result.id]?.trim() ?? ""
    if (!reason) {
      setActionError(t("screeningQueue.rationaleRequired"))
      return
    }
    setActing(result.id)
    setActionError(null)
    try {
      await api.customers.reviewScreeningResult(result.id, {
        status: next,
        rationale: reason,
        false_positive_reason: next === "FALSE_POSITIVE" ? reason : undefined,
        expected_version: result.version,
      })
      setRationale((current) => ({ ...current, [result.id]: "" }))
      setRefreshKey((key) => key + 1)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setActing(null)
    }
  }

  if (loading) return <div className="space-y-6"><div className="h-8 w-56 animate-pulse rounded bg-muted" /><div className="h-64 animate-pulse rounded-xl border bg-muted" /></div>
  if (error) return <div className="space-y-3 p-12 text-center"><p role="alert" className="text-destructive">{t("screeningQueue.error")}: {error}</p><Button variant="outline" onClick={() => setRefreshKey((key) => key + 1)}><RefreshCw className="h-4 w-4" />{t("screeningQueue.retry")}</Button></div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("screeningQueue.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("screeningQueue.description")}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setRefreshKey((key) => key + 1)}><RefreshCw className="h-4 w-4" />{t("screeningQueue.refresh")}</Button>
      </div>
      {actionError && <p role="alert" className="rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">{actionError}</p>}
      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base">
            <ShieldAlert className="h-4 w-4" />
            {t("screeningQueue.sources.title")}
          </CardTitle>
          {sources && (
            <Badge variant={sources.screening_ready ? "low" : "destructive"}>
              {sources.screening_ready ? t("screeningQueue.sources.ready") : t("screeningQueue.sources.notReady")}
            </Badge>
          )}
        </CardHeader>
        <CardContent className="space-y-3">
          {sourcesError ? <p role="alert" className="text-sm text-destructive">{t("screeningQueue.sources.error", { error: sourcesError })}</p> : !sources ? <p role="status" className="text-sm text-muted-foreground">{t("screeningQueue.sources.loading")}</p> : (
            <>
              {!sources.screening_ready && (
                <p role="alert" className="rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">
                  {t("screeningQueue.sources.degradedWarning", { sources: (sources.degraded_sources ?? []).join(", ") })}
                </p>
              )}
              <p className="text-xs text-muted-foreground">
                {t("screeningQueue.sources.counts", { configured: sources.configured_count, ready: sources.ready_count, unready: sources.unready_count })}
                {sources.policy_version ? ` · ${t("screeningQueue.sources.policyVersion", { version: sources.policy_version })}` : ""}
              </p>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader><TableRow><TableHead>{t("screeningQueue.sources.listId")}</TableHead><TableHead>{t("screeningQueue.sources.requirement")}</TableHead><TableHead>{t("screeningQueue.sources.state")}</TableHead><TableHead>{t("screeningQueue.sources.lastSuccess")}</TableHead><TableHead>{t("screeningQueue.sources.lastAttempt")}</TableHead><TableHead>{t("screeningQueue.sources.age")}</TableHead><TableHead>{t("screeningQueue.sources.failures")}</TableHead><TableHead>{t("screeningQueue.sources.diagnostic")}</TableHead></TableRow></TableHeader>
                  <TableBody>{sources.data.map((source) => (
                    <TableRow key={source.list_id}>
                      <TableCell className="font-mono text-xs">{source.list_id}<div className="text-xs text-muted-foreground">{source.list_type}</div></TableCell>
                      <TableCell><Badge variant={requiredSources.has(source.list_id) ? "outline" : "secondary"}>{requiredSources.has(source.list_id) ? t("screeningQueue.sources.required") : t("screeningQueue.sources.optional")}</Badge></TableCell>
                      <TableCell><Badge variant={source.operational_state === "ready" ? "low" : requiredSources.has(source.list_id) ? "critical" : "medium"}>{t(`screeningQueue.sources.operationalState.${source.operational_state}`, { defaultValue: source.operational_state })}</Badge></TableCell>
                      <TableCell className="whitespace-nowrap text-xs">{source.last_success_at ? new Date(source.last_success_at).toLocaleString(i18n.language) : t("screeningQueue.sources.never")}</TableCell>
                      {/* Last attempt separate from last success: a source
                          that is being tried every hour and failing every time
                          looks identical to an abandoned one when only the
                          last success is shown. */}
                      <TableCell className="whitespace-nowrap text-xs" data-testid={`source-last-attempt-${source.list_id}`}>{source.last_attempt_at ? new Date(source.last_attempt_at).toLocaleString(i18n.language) : t("screeningQueue.sources.never")}</TableCell>
                      {/* Age against the configured threshold, not age alone:
                          "26 hours" means nothing without the window it is
                          being judged against. */}
                      <TableCell className="whitespace-nowrap text-xs" data-testid={`source-age-${source.list_id}`}>
                        {source.age_seconds == null
                          ? "-"
                          : t("screeningQueue.sources.ageOverThreshold", {
                              age: formatDuration(source.age_seconds, t),
                              threshold: formatDuration(source.freshness_threshold_seconds, t),
                            })}
                      </TableCell>
                      <TableCell className="text-xs" data-testid={`source-failures-${source.list_id}`}>{source.consecutive_failures ?? 0}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{source.diagnostic ?? "-"}</TableCell>
                    </TableRow>
                  ))}</TableBody>
                </Table>
              </div>
            </>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base"><ShieldAlert className="h-4 w-4" />{t("screeningQueue.title")}</CardTitle>
          <div className="flex flex-wrap items-center gap-4">
            <label className="flex items-center gap-2 text-sm" htmlFor="screening-status-filter">
              <span>{t("screeningQueue.filter")}</span>
              <select id="screening-status-filter" value={status} onChange={(event) => setStatus(event.target.value as ScreeningResultStatus | "")} className="rounded-md border bg-background px-2 py-1">
                {statuses.map((item) => <option key={item || "all"} value={item}>{item ? t(`screeningQueue.status.${item}`) : t("screeningQueue.all")}</option>)}
              </select>
            </label>
            <label className="flex items-center gap-2 text-sm" htmlFor="screening-show-suppressed">
              <input id="screening-show-suppressed" type="checkbox" checked={showSuppressed} onChange={(event) => setShowSuppressed(event.target.checked)} />
              <span>{t("screeningQueue.showSuppressed")}</span>
            </label>
          </div>
        </CardHeader>
        <CardContent>
          {page?.data.length === 0 ? <p role="status" className="py-8 text-center text-sm text-muted-foreground">{t("screeningQueue.empty")}</p> : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader><TableRow><TableHead>{t("screeningQueue.customer")}</TableHead><TableHead>{t("screeningQueue.match")}</TableHead><TableHead>{t("screeningQueue.statusLabel")}</TableHead><TableHead>{t("screeningQueue.screenedAt")}</TableHead><TableHead>{t("screeningQueue.action")}</TableHead></TableRow></TableHeader>
                <TableBody>{page?.data.map((result) => (
                  <TableRow key={result.id}>
                    <TableCell><Link className="text-primary hover:underline" to={`/customers/${result.customer_id}`}>{result.customer_id}</Link><div className="text-xs text-muted-foreground">{result.list_id}</div></TableCell>
                    <TableCell><div>{result.matched_name}</div><div className="text-xs text-muted-foreground">{(result.similarity * 100).toFixed(1)}%</div>{result.suppressed && <Badge variant="secondary">{t("screeningQueue.suppressed")}</Badge>}{result.degraded && <Badge variant="destructive" title={t("screeningQueue.degradedSources", { sources: (result.degraded_sources ?? []).join(", ") })}>{t("screeningQueue.degraded")}</Badge>}{result.degraded && <div className="text-xs text-destructive">{t("screeningQueue.degradedSources", { sources: (result.degraded_sources ?? []).join(", ") })}</div>}</TableCell>
                    <TableCell><Badge variant={result.status === "TRUE_POSITIVE" ? "critical" : result.status === "FALSE_POSITIVE" ? "secondary" : "outline"}>{t(`screeningQueue.status.${result.status}`)}</Badge>{result.case_id && <Link className="ml-2 text-xs text-primary hover:underline" to={`/cases/${result.case_id}`}>{t("screeningQueue.case")}</Link>}</TableCell>
                    <TableCell className="whitespace-nowrap text-xs">{new Date(result.screened_at).toLocaleString(i18n.language)}</TableCell>
                    <TableCell className="min-w-64">
                      {(result.status === "NEW" || result.status === "REVIEWING") && <div className="space-y-2"><label className="sr-only" htmlFor={`screening-rationale-${result.id}`}>{t("screeningQueue.rationale")}</label><textarea id={`screening-rationale-${result.id}`} value={rationale[result.id] ?? ""} onChange={(event) => setRationale((current) => ({ ...current, [result.id]: event.target.value }))} placeholder={t("screeningQueue.rationalePlaceholder")} className="min-h-16 w-full rounded-md border bg-background px-2 py-1 text-xs" />{result.status === "NEW" ? <Button size="sm" variant="outline" disabled={acting === result.id} onClick={() => review(result, "REVIEWING")}>{t("screeningQueue.startReview")}</Button> : <div className="flex gap-2"><Button size="sm" disabled={acting === result.id} onClick={() => review(result, "TRUE_POSITIVE")}>{t("screeningQueue.truePositive")}</Button><Button size="sm" variant="outline" disabled={acting === result.id} onClick={() => review(result, "FALSE_POSITIVE")}>{t("screeningQueue.falsePositive")}</Button></div>}</div>}
                    </TableCell>
                  </TableRow>
                ))}</TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
