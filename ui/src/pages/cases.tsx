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
import { api, type CasePriority } from "@/lib/api"
import { Plus } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router-dom"

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function CasesPage() {
  const { t, i18n } = useTranslation()
  const priorityLabels: Record<CasePriority, string> = {
    low: t("casePriority.low"),
    medium: t("casePriority.medium"),
    high: t("casePriority.high"),
    critical: t("casePriority.critical"),
  }
  const statusLabels: Record<string, string> = {
    open: t("caseStatus.open"),
    new: t("caseStatus.new"),
    investigating: t("caseStatus.investigating"),
    escalated: t("caseStatus.escalated"),
    closed: t("caseStatus.closed"),
    reopened: t("caseStatus.reopened"),
    str_filed: t("caseStatus.str_filed"),
  }
  const { data: cases, loading, error } = useApi(api.cases.list)
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [priority, setPriority] = useState<CasePriority>("medium")
  const custRef = useRef<HTMLInputElement>(null)
  const alertRef = useRef<HTMLInputElement>(null)
  const assignRef = useRef<HTMLInputElement>(null)
  const summaryRef = useRef<HTMLInputElement>(null)

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const customerId = custRef.current?.value.trim()
    const summary = summaryRef.current?.value.trim()
    if (!customerId || !summary) return
    setCreating(true)
    const alertIds = alertRef.current?.value.trim()
      ? alertRef.current.value.split(",").map((s) => s.trim()).filter(Boolean)
      : []
    try {
      await api.cases.create({
        customer_id: customerId,
        alert_ids: alertIds,
        priority,
        assigned_to: assignRef.current?.value.trim() || undefined,
        summary,
      })
      window.location.reload()
    } finally {
      setCreating(false)
    }
  }

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">{t("cases.error")}</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("cases.title")}</h1>
        <div className="flex items-center gap-2">
          <p className="text-sm text-muted-foreground">{t("cases.count", { count: cases?.length ?? 0 })}</p>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            {t("cases.createButton")}
          </Button>
        </div>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("cases.form.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-3">
              <div className="flex flex-wrap items-end gap-3">
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.customerId")}</label>
                  <input ref={custRef} required placeholder="cust-001"
                    className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.alertIds")}</label>
                  <input ref={alertRef} placeholder="alert-001,alert-002"
                    className="w-48 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.assignedTo")}</label>
                  <input ref={assignRef} placeholder="tanaka"
                    className="w-24 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.priority")}</label>
                  <div className="flex gap-1">
                    {(["low", "medium", "high"] as const).map((p) => (
                      <button key={p} type="button" onClick={() => setPriority(p)}
                        className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors ${priority === p ? "border-primary bg-primary/10 text-primary" : "border-input text-muted-foreground hover:bg-accent"}`}>
                        {priorityLabels[p]}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("cases.form.summary")}</label>
                <input ref={summaryRef} required placeholder={t("cases.form.summaryPlaceholder")}
                  className="w-full rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>{t("cases.form.submit")}</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("cases.table.header.priority")}</TableHead>
              <TableHead>{t("cases.table.header.status")}</TableHead>
              <TableHead>{t("cases.table.header.customerId")}</TableHead>
              <TableHead>{t("cases.table.header.assignedTo")}</TableHead>
              <TableHead>{t("cases.table.header.summary")}</TableHead>
              <TableHead>{t("cases.table.header.createdAt")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cases && cases.length > 0 ? (
              cases.map((c) => (
                <TableRow key={c.id} className="cursor-pointer">
                  <TableCell>
                    <Link to={`/cases/${c.id}`}>
                      <Badge variant={PRIORITY_VARIANT[c.priority]}>
                        {priorityLabels[c.priority] ?? c.priority}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{statusLabels[c.status] ?? c.status}</Badge>
                  </TableCell>
                  <TableCell className="font-mono text-sm">
                    <Link to={`/customers/${c.customer_id}`} className="text-primary hover:underline">
                      {c.customer_id}
                    </Link>
                  </TableCell>
                  <TableCell>{c.assigned_to || "-"}</TableCell>
                  <TableCell className="max-w-[300px] truncate">{c.summary}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(c.created_at, i18n.language)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  {t("cases.table.empty")}
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
