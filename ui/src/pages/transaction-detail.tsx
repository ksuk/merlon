import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
import { ArrowLeft } from "lucide-react"
import { useCallback } from "react"
import { Link, useParams } from "react-router-dom"

const DIR_LABELS: Record<string, string> = {
  inbound: "入金",
  outbound: "出金",
  internal: "内部",
}

const DIR_VARIANT: Record<string, "low" | "medium" | "high"> = {
  inbound: "low",
  outbound: "high",
  internal: "medium",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

function formatAmount(amount: number, currency: string) {
  return new Intl.NumberFormat("ja-JP", { style: "currency", currency }).format(amount)
}

export function TransactionDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { data: txn, loading, error } = useApi(
    useCallback(() => api.transactions.get(id!), [id]),
  )

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-64 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error || !txn) {
    return (
      <div className="space-y-4">
        <Link to="/transactions" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> 取引一覧に戻る
        </Link>
        <p className="text-destructive">取引データの取得に失敗しました</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/transactions" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> 戻る
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{txn.external_id}</h1>
        <Badge variant={DIR_VARIANT[txn.direction] ?? "secondary"}>
          {DIR_LABELS[txn.direction] ?? txn.direction}
        </Badge>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">取引情報</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">内部ID</dt>
                <dd className="font-mono">{txn.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">外部ID</dt>
                <dd className="font-mono">{txn.external_id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">顧客ID</dt>
                <dd>
                  <Link to={`/customers/${txn.customer_id}`} className="text-primary hover:underline font-mono">
                    {txn.customer_id}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">方向</dt>
                <dd><Badge variant={DIR_VARIANT[txn.direction]}>{DIR_LABELS[txn.direction]}</Badge></dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">金額・経路</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">金額</dt>
                <dd className="text-xl font-bold">{formatAmount(txn.amount, txn.currency)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">通貨</dt>
                <dd>{txn.currency}</dd>
              </div>
              {txn.counterparty_country && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">相手先国</dt>
                  <dd>{txn.counterparty_country}</dd>
                </div>
              )}
              {txn.channel && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">チャネル</dt>
                  <dd>{txn.channel}</dd>
                </div>
              )}
              <div className="flex justify-between">
                <dt className="text-muted-foreground">実行日時</dt>
                <dd>{formatDateTime(txn.executed_at)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">登録日時</dt>
                <dd>{formatDateTime(txn.created_at)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
