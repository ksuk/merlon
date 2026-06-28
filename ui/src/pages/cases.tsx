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
import { api, type CasePriority, type CaseStatus } from "@/lib/api"
import { Plus } from "lucide-react"
import { useRef, useState } from "react"
import { Link } from "react-router-dom"

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high"> = {
  low: "low",
  medium: "medium",
  high: "high",
}

const PRIORITY_LABELS: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
}

const STATUS_LABELS: Record<CaseStatus, string> = {
  open: "未対応",
  investigating: "調査中",
  escalated: "エスカレーション",
  closed: "完了",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function CasesPage() {
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
    return <p className="p-12 text-center text-destructive">ケースデータの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">ケース一覧</h1>
        <div className="flex items-center gap-2">
          <p className="text-sm text-muted-foreground">{cases?.length ?? 0} 件</p>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            新規作成
          </Button>
        </div>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">ケース作成</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-3">
              <div className="flex flex-wrap items-end gap-3">
                <div>
                  <label className="mb-1 block text-xs font-medium">顧客ID</label>
                  <input ref={custRef} required placeholder="cust-001"
                    className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">アラートID（カンマ区切り）</label>
                  <input ref={alertRef} placeholder="alert-001,alert-002"
                    className="w-48 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">担当者</label>
                  <input ref={assignRef} placeholder="tanaka"
                    className="w-24 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">優先度</label>
                  <div className="flex gap-1">
                    {(["low", "medium", "high"] as const).map((p) => (
                      <button key={p} type="button" onClick={() => setPriority(p)}
                        className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors ${priority === p ? "border-primary bg-primary/10 text-primary" : "border-input text-muted-foreground hover:bg-accent"}`}>
                        {PRIORITY_LABELS[p]}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">概要</label>
                <input ref={summaryRef} required placeholder="ケースの概要..."
                  className="w-full rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>作成</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>優先度</TableHead>
              <TableHead>ステータス</TableHead>
              <TableHead>顧客ID</TableHead>
              <TableHead>担当者</TableHead>
              <TableHead>概要</TableHead>
              <TableHead>作成日時</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cases && cases.length > 0 ? (
              cases.map((c) => (
                <TableRow key={c.id} className="cursor-pointer">
                  <TableCell>
                    <Link to={`/cases/${c.id}`}>
                      <Badge variant={PRIORITY_VARIANT[c.priority]}>
                        {PRIORITY_LABELS[c.priority] ?? c.priority}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{STATUS_LABELS[c.status] ?? c.status}</Badge>
                  </TableCell>
                  <TableCell className="font-mono text-sm">
                    <Link to={`/customers/${c.customer_id}`} className="text-primary hover:underline">
                      {c.customer_id}
                    </Link>
                  </TableCell>
                  <TableCell>{c.assigned_to || "-"}</TableCell>
                  <TableCell className="max-w-[300px] truncate">{c.summary}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(c.created_at)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  ケースがありません
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
