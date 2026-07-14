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
import { api, type BacktestJob, type BacktestResult, type Customer } from "@/lib/api"
import { FlaskConical, Play } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"

export function BacktestPage() {
  const { t } = useTranslation()
  const { data: customers, loading, error } = useApi(api.customers.list)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<BacktestResult | null>(null)
  const [job, setJob] = useState<BacktestJob | null>(null)
  const [from, setFrom] = useState(() => new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10))
  const [to, setTo] = useState(() => new Date().toISOString().slice(0, 10))
  const descRef = useRef<HTMLInputElement>(null)
  const candidateRef = useRef<HTMLInputElement>(null)

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

  async function handleRun() {
    if (selectedIds.length === 0) return
    setRunning(true)
    setResult(null)
    try {
      const candidate = candidateRef.current?.value.trim() || ""
      if (!candidate) return
      const created = await api.backtest.create({
        from: `${from}T00:00:00Z`,
        to: `${to}T00:00:00Z`,
        customer_ids: selectedIds,
        scenario_ids: [],
        baseline_rule_set_id: "active",
        candidate_rule_set_id: candidate,
      })
      setJob(created)
      let current = created
      while (current.status === "queued" || current.status === "running") {
        await new Promise((resolve) => setTimeout(resolve, 1000))
        current = await api.backtest.get(current.id)
        setJob(current)
      }
      if (current.status === "completed" && current.candidate) setResult(current.candidate)
    } finally {
      setRunning(false)
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
    return <p className="p-12 text-center text-destructive">{t("backtest.error")}</p>
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
            <label className="mb-1 block text-sm font-medium">{t("backtest.form.descriptionLabel")}</label>
            <input
              ref={descRef}
              placeholder={t("backtest.form.descriptionPlaceholder")}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <label className="text-sm font-medium">From (UTC)
              <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm" />
            </label>
            <label className="text-sm font-medium">To (UTC, exclusive)
              <input type="date" value={to} onChange={(e) => setTo(e.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm" />
            </label>
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium">Candidate rule set</label>
            <input ref={candidateRef} placeholder="active TM rule name" required className="w-full rounded-md border bg-background px-3 py-2 text-sm" />
          </div>
          <div>
            <div className="mb-2 flex items-center justify-between">
              <label className="text-sm font-medium">{t("backtest.form.targetCustomers")}</label>
              <Button variant="ghost" size="sm" onClick={selectAll}>
                {selectedIds.length === (customers?.length ?? 0) ? t("backtest.form.deselectAll") : t("backtest.form.selectAll")}
              </Button>
            </div>
            <div className="max-h-48 space-y-1 overflow-y-auto">
              {customers?.map((c: Customer) => (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => toggleCustomer(c.id)}
                  className={`flex w-full items-center gap-3 rounded-md border p-2 text-left text-sm transition-colors ${
                    selectedIds.includes(c.id)
                      ? "border-primary bg-primary/5"
                      : "hover:bg-accent"
                  }`}
                >
                  <div className={`h-3 w-3 rounded-sm border ${selectedIds.includes(c.id) ? "border-primary bg-primary" : "border-input"}`} />
                  <span className="font-mono text-xs">{c.external_id}</span>
                  <span className="text-muted-foreground">{c.country_code}</span>
                </button>
              ))}
            </div>
          </div>
          <Button size="sm" disabled={running || selectedIds.length === 0} onClick={handleRun}>
            <Play className="h-4 w-4" />
            {running ? t("backtest.form.running") : t("backtest.form.submit")}
          </Button>
          {job && <p className="text-xs text-muted-foreground">Job {job.id}: {job.status} ({Math.round(job.progress * 100)}%)</p>}
        </CardContent>
      </Card>

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

            {result.scenario_results.length > 0 && (
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
                  {result.scenario_results.map((s) => (
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
