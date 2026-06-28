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

const DIRECTION_LABELS: Record<string, string> = {
  inbound: "入金",
  outbound: "出金",
  internal: "内部",
}

const DIRECTION_VARIANT: Record<string, "low" | "high" | "secondary"> = {
  inbound: "low",
  outbound: "high",
  internal: "secondary",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

function formatAmount(amount: number, currency: string) {
  return new Intl.NumberFormat("ja-JP", { style: "currency", currency }).format(amount)
}

export function TransactionsPage() {
  const { data: transactions, loading, error } = useApi(api.transactions.list)

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">取引データの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">取引一覧</h1>
        <p className="text-sm text-muted-foreground">{transactions?.length ?? 0} 件</p>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>方向</TableHead>
              <TableHead>顧客ID</TableHead>
              <TableHead>金額</TableHead>
              <TableHead>相手先国</TableHead>
              <TableHead>チャネル</TableHead>
              <TableHead>実行日時</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {transactions && transactions.length > 0 ? (
              transactions.map((t) => (
                <TableRow key={t.id}>
                  <TableCell>
                    <Badge variant={DIRECTION_VARIANT[t.direction] ?? "secondary"}>
                      {DIRECTION_LABELS[t.direction] ?? t.direction}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-sm">{t.customer_id}</TableCell>
                  <TableCell className="font-mono">
                    {formatAmount(t.amount, t.currency)}
                  </TableCell>
                  <TableCell>{t.counterparty_country || "-"}</TableCell>
                  <TableCell>{t.channel || "-"}</TableCell>
                  <TableCell className="whitespace-nowrap">
                    {formatDateTime(t.executed_at)}
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  取引データがありません
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
