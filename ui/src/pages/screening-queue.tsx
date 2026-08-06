import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type ScreeningResultRecord, type ScreeningResultStatus } from "@/lib/api"
import { RefreshCw, ShieldAlert } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

const statuses: Array<ScreeningResultStatus | ""> = ["", "NEW", "REVIEWING", "TRUE_POSITIVE", "FALSE_POSITIVE"]

export function ScreeningQueuePage() {
  const { t, i18n } = useTranslation()
  const [status, setStatus] = useState<ScreeningResultStatus | "">("")
  const [refreshKey, setRefreshKey] = useState(0)
  const [acting, setActing] = useState<string | null>(null)
  const [rationale, setRationale] = useState<Record<string, string>>({})
  const [actionError, setActionError] = useState<string | null>(null)
  const { data: page, loading, error } = useApi(
    () => api.screening.results({ status: status || undefined, limit: 50 }),
    `${status}:${refreshKey}`,
  )

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
          <CardTitle className="flex items-center gap-2 text-base"><ShieldAlert className="h-4 w-4" />{t("screeningQueue.title")}</CardTitle>
          <label className="flex items-center gap-2 text-sm" htmlFor="screening-status-filter">
            <span>{t("screeningQueue.filter")}</span>
            <select id="screening-status-filter" value={status} onChange={(event) => setStatus(event.target.value as ScreeningResultStatus | "")} className="rounded-md border bg-background px-2 py-1">
              {statuses.map((item) => <option key={item || "all"} value={item}>{item ? t(`screeningQueue.status.${item}`) : t("screeningQueue.all")}</option>)}
            </select>
          </label>
        </CardHeader>
        <CardContent>
          {page?.data.length === 0 ? <p role="status" className="py-8 text-center text-sm text-muted-foreground">{t("screeningQueue.empty")}</p> : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader><TableRow><TableHead>{t("screeningQueue.customer")}</TableHead><TableHead>{t("screeningQueue.match")}</TableHead><TableHead>{t("screeningQueue.statusLabel")}</TableHead><TableHead>{t("screeningQueue.screenedAt")}</TableHead><TableHead>{t("screeningQueue.action")}</TableHead></TableRow></TableHeader>
                <TableBody>{page?.data.map((result) => (
                  <TableRow key={result.id}>
                    <TableCell><Link className="text-primary hover:underline" to={`/customers/${result.customer_id}`}>{result.customer_id}</Link><div className="text-xs text-muted-foreground">{result.list_id}</div></TableCell>
                    <TableCell><div>{result.matched_name}</div><div className="text-xs text-muted-foreground">{(result.similarity * 100).toFixed(1)}%</div>{result.suppressed && <Badge variant="secondary">{t("screeningQueue.suppressed")}</Badge>}</TableCell>
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
