import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type CoverageAnalysis, type CoverageAnalysisStatus, type CoverageMatterResult } from "@/lib/api"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

const statuses: CoverageAnalysisStatus[] = ["queued", "running", "completed", "failed"]

export function CoverageAnalysesPage() {
  const { t, i18n } = useTranslation()
  const [status, setStatus] = useState<CoverageAnalysisStatus | "">("")
  const [refresh, setRefresh] = useState(0)
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const { data: page, loading, error } = useApi(
    () => api.coverageAnalyses.list({ status: status || undefined, limit: 50 }),
    `${status}:${refresh}`,
  )
  const selected = page?.data.find((item) => item.id === selectedID) ?? null
  const { data: selectedAnalysis } = useApi(
    () => selectedID ? api.coverageAnalyses.get(selectedID) : Promise.resolve(null),
    selectedID,
  )
  const analysis = selectedAnalysis ?? selected
  const { data: matters } = useApi(
    () => selectedID ? api.coverageAnalyses.matters(selectedID, { limit: 100 }) : Promise.resolve({ data: [], pagination: { has_more: false } }),
    selectedID,
  )

  async function queueAnalysis() {
    try {
      const created = await api.coverageAnalyses.create({})
      setSelectedID(created.id)
      setRefresh((value) => value + 1)
    } catch {
      // The list remains usable; the server error is surfaced by the next refresh.
    }
  }

  if (loading && !page) return <div role="status" className="p-12 text-center text-muted-foreground">{t("coverage.loading")}</div>
  if (error) return <p role="alert" className="p-12 text-center text-destructive">{t("coverage.error", { error })}</p>

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h1 className="text-2xl font-bold tracking-tight">{t("coverage.title")}</h1><p className="text-sm text-muted-foreground">{t("coverage.subtitle")}</p></div>
        <Button onClick={() => void queueAnalysis()}>{t("coverage.queue")}</Button>
      </div>

      <Card>
        <CardHeader className="flex-row items-center justify-between"><CardTitle className="text-base">{t("coverage.queueTitle")}</CardTitle><label className="text-sm"><span className="sr-only">{t("coverage.filter")}</span><select value={status} onChange={(event) => setStatus(event.target.value as CoverageAnalysisStatus | "")} className="rounded-md border bg-background px-2 py-1"><option value="">{t("coverage.allStatuses")}</option>{statuses.map((value) => <option key={value} value={value}>{t(`coverage.status.${value}`)}</option>)}</select></label></CardHeader>
        <CardContent><div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>{t("coverage.table.id")}</TableHead><TableHead>{t("coverage.table.status")}</TableHead><TableHead>{t("coverage.table.knownMatter")}</TableHead><TableHead>{t("coverage.table.rate")}</TableHead><TableHead>{t("coverage.table.snapshot")}</TableHead></TableRow></TableHeader><TableBody>{(page?.data ?? []).length === 0 ? <TableRow><TableCell colSpan={5} className="h-24 text-center text-muted-foreground">{t("coverage.empty")}</TableCell></TableRow> : page?.data.map((item) => <TableRow key={item.id} className="cursor-pointer" onClick={() => setSelectedID(item.id)}><TableCell className="font-mono text-xs">{item.id}</TableCell><TableCell><Badge variant={item.status === "failed" ? "critical" : item.status === "completed" ? "low" : "outline"}>{t(`coverage.status.${item.status}`)}</Badge></TableCell><TableCell>{item.summary.known_matter}</TableCell><TableCell>{formatRate(item.summary.rate)} <span className="text-xs text-muted-foreground">({item.summary.covered}/{item.summary.denominator})</span></TableCell><TableCell>{formatDate(item.snapshot_at, i18n.language)}</TableCell></TableRow>)}</TableBody></Table></div></CardContent>
      </Card>

      {analysis && <CoverageDetail analysis={analysis} matters={matters?.data ?? []} locale={i18n.language} />}
    </div>
  )
}

function CoverageDetail({ analysis, matters, locale }: { analysis: CoverageAnalysis; matters: CoverageMatterResult[]; locale: string }) {
  const { t } = useTranslation()
  const summary = analysis.summary
  return <Card data-testid="coverage-analysis-detail"><CardHeader><CardTitle className="text-base">{t("coverage.detail.title", { id: analysis.id })}</CardTitle></CardHeader><CardContent className="space-y-4"><p className="text-sm text-muted-foreground">{t("coverage.boundary")}</p><div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5"><Stat label={t("coverage.stats.knownMatter")} value={summary.known_matter} /><Stat label={t("coverage.stats.covered")} value={summary.covered} /><Stat label={t("coverage.stats.notCovered")} value={summary.not_covered} /><Stat label={t("coverage.stats.unevaluable")} value={summary.unevaluable} /><Stat label={t("coverage.stats.rate")} value={`${formatRate(summary.rate)} (${summary.covered}/${summary.denominator})`} /></div><div className="rounded-md border p-3 text-xs text-muted-foreground"><p>{t("coverage.matcher", { version: analysis.matcher_version })}</p><p>{t("coverage.snapshot", { value: formatDate(analysis.snapshot_at, locale) })}</p>{analysis.assumptions.map((assumption) => <p key={assumption}>- {assumption}</p>)}</div>{analysis.error && <p role="alert" className="text-sm text-destructive">{analysis.error}</p>}{matters.length > 0 && <div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>{t("coverage.table.matter")}</TableHead><TableHead>{t("coverage.table.customer")}</TableHead><TableHead>{t("coverage.table.source")}</TableHead><TableHead>{t("coverage.table.label")}</TableHead><TableHead>{t("coverage.table.covered")}</TableHead></TableRow></TableHeader><TableBody>{matters.map((matter) => <TableRow key={matter.id}><TableCell className="font-mono text-xs">{matter.matter_id}</TableCell><TableCell><Link to={`/customers/${matter.customer_id}`} className="font-mono text-primary hover:underline">{matter.customer_id}</Link></TableCell><TableCell>{matter.source || "-"}</TableCell><TableCell><Badge variant={matter.label === "TP" ? "low" : matter.label === "unevaluable" ? "outline" : "critical"}>{matter.label}</Badge></TableCell><TableCell>{matter.unevaluable ? t("coverage.unevaluable") : matter.covered ? t("coverage.covered") : t("coverage.notCovered")}</TableCell></TableRow>)}</TableBody></Table></div>}</CardContent></Card>
}

function Stat({ label, value }: { label: string; value: string | number }) {
  return <div className="rounded-md border p-3 text-center"><p className="text-xl font-bold">{value}</p><p className="text-xs text-muted-foreground">{label}</p></div>
}

function formatRate(rate: number) {
  return `${(rate * 100).toFixed(1)}%`
}

function formatDate(value: string, locale: string) {
  return new Date(value).toLocaleString(locale)
}
