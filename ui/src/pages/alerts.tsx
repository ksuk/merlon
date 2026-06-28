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
import { api, type AlertSeverity, type AlertStatus } from "@/lib/api"
import { Link } from "react-router-dom"

const SEVERITY_VARIANT: Record<AlertSeverity, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

const SEVERITY_LABELS: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
  critical: "重大",
}

const STATUS_LABELS: Record<AlertStatus, string> = {
  open: "未対応",
  investigating: "調査中",
  escalated: "エスカレーション",
  closed_true_positive: "完了(真陽性)",
  closed_false_positive: "完了(偽陽性)",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function AlertsPage() {
  const { data: alerts, loading, error } = useApi(api.alerts.list)

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">アラートデータの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">アラート一覧</h1>
        <p className="text-sm text-muted-foreground">{alerts?.length ?? 0} 件</p>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>深刻度</TableHead>
              <TableHead>ステータス</TableHead>
              <TableHead>顧客ID</TableHead>
              <TableHead>シナリオ</TableHead>
              <TableHead>スコア</TableHead>
              <TableHead>説明</TableHead>
              <TableHead>検出日時</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {alerts && alerts.length > 0 ? (
              alerts.map((a) => (
                <TableRow key={a.id} className="cursor-pointer">
                  <TableCell>
                    <Link to={`/alerts/${a.id}`}>
                      <Badge variant={SEVERITY_VARIANT[a.severity]}>
                        {SEVERITY_LABELS[a.severity] ?? a.severity}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {STATUS_LABELS[a.status] ?? a.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-sm">{a.customer_id}</TableCell>
                  <TableCell className="font-mono text-sm">{a.scenario_id}</TableCell>
                  <TableCell>{a.score.toFixed(1)}</TableCell>
                  <TableCell className="max-w-[300px] truncate">{a.description}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(a.detected_at)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={7} className="h-24 text-center text-muted-foreground">
                  アラートがありません
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
