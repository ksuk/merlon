import { Badge } from "@/components/ui/badge"
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
        <p className="text-sm text-muted-foreground">{cases?.length ?? 0} 件</p>
      </div>

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
                <TableRow key={c.id}>
                  <TableCell>
                    <Badge variant={PRIORITY_VARIANT[c.priority]}>
                      {PRIORITY_LABELS[c.priority] ?? c.priority}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {STATUS_LABELS[c.status] ?? c.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-sm">{c.customer_id}</TableCell>
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
