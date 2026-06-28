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
import { api } from "@/lib/api"

const ACTION_LABELS: Record<string, string> = {
  create: "作成",
  update: "更新",
  update_status: "ステータス変更",
  delete: "削除",
  score_customer: "スコアリング",
  screen_customer: "スクリーニング",
  run_backtest: "バックテスト",
  create_str: "STR作成",
}

const RESOURCE_LABELS: Record<string, string> = {
  customers: "顧客",
  transactions: "取引",
  alerts: "アラート",
  cases: "ケース",
  webhooks: "Webhook",
  batch: "バッチ",
  reports: "レポート",
  admin: "管理",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function AuditPage() {
  const { data: entries, loading, error } = useApi(api.audit.list)

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">監査ログの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">監査ログ</h1>
        <p className="text-sm text-muted-foreground">{entries?.length ?? 0} 件</p>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>日時</TableHead>
              <TableHead>操作</TableHead>
              <TableHead>リソース</TableHead>
              <TableHead>リソースID</TableHead>
              <TableHead>ユーザー</TableHead>
              <TableHead>IPアドレス</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries && entries.length > 0 ? (
              entries.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className="whitespace-nowrap text-sm">
                    {formatDateTime(e.created_at)}
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">
                      {ACTION_LABELS[e.action] ?? e.action}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {RESOURCE_LABELS[e.resource_type] ?? e.resource_type}
                  </TableCell>
                  <TableCell className="font-mono text-sm">
                    {e.resource_id || "-"}
                  </TableCell>
                  <TableCell>{e.user_id || "-"}</TableCell>
                  <TableCell className="font-mono text-sm">
                    {e.ip_address || "-"}
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  監査ログがありません
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
