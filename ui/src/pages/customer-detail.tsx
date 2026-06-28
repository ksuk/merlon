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
import { api, type RiskTier } from "@/lib/api"
import { ArrowLeft, RefreshCw } from "lucide-react"
import { useCallback, useState } from "react"
import { Link, useParams } from "react-router-dom"

const TIER_VARIANT: Record<RiskTier, "low" | "medium" | "high"> = {
  low: "low",
  medium: "medium",
  high: "high",
}

const TIER_LABELS: Record<string, string> = {
  low: "低リスク",
  medium: "中リスク",
  high: "高リスク",
}

const TYPE_LABELS: Record<string, string> = {
  individual: "個人",
  corporate_domestic: "国内法人",
  corporate_foreign: "海外法人",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function CustomerDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { data: customer, loading, error } = useApi(
    useCallback(() => api.customers.get(id!), [id]),
  )
  const { data: scores, loading: scoresLoading } = useApi(
    useCallback(() => api.customers.scoreHistory(id!), [id]),
  )
  const [scoring, setScoring] = useState(false)

  async function handleScore() {
    if (!id) return
    setScoring(true)
    try {
      await api.customers.score(id, "default")
      window.location.reload()
    } catch {
      setScoring(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-64 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error || !customer) {
    return (
      <div className="space-y-4">
        <Link to="/customers" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> 顧客一覧に戻る
        </Link>
        <p className="text-destructive">顧客データの取得に失敗しました</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/customers" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> 戻る
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{customer.external_id}</h1>
        {customer.risk_tier && (
          <Badge variant={TIER_VARIANT[customer.risk_tier]}>
            {TIER_LABELS[customer.risk_tier]}
          </Badge>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">基本情報</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">内部ID</dt>
                <dd className="font-mono">{customer.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">種別</dt>
                <dd>{TYPE_LABELS[customer.customer_type] ?? customer.customer_type}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">国コード</dt>
                <dd>{customer.country_code}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">プロダクト</dt>
                <dd>{customer.product_types?.join(", ") || "-"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">作成日</dt>
                <dd>{formatDateTime(customer.created_at)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">更新日</dt>
                <dd>{formatDateTime(customer.updated_at)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-base">リスク評価</CardTitle>
            <Button size="sm" variant="outline" onClick={handleScore} disabled={scoring}>
              <RefreshCw className={`h-4 w-4 ${scoring ? "animate-spin" : ""}`} />
              スコアリング
            </Button>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">リスクスコア</dt>
                <dd className="text-2xl font-bold">
                  {customer.risk_score != null ? customer.risk_score.toFixed(1) : "-"}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">リスクティア</dt>
                <dd>
                  {customer.risk_tier ? (
                    <Badge variant={TIER_VARIANT[customer.risk_tier]}>
                      {TIER_LABELS[customer.risk_tier]}
                    </Badge>
                  ) : (
                    "未スコア"
                  )}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">最終スコアリング</dt>
                <dd>{customer.last_scored_at ? formatDateTime(customer.last_scored_at) : "-"}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>

      {customer.attributes && Object.keys(customer.attributes).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">属性</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-2 text-sm md:grid-cols-2">
              {Object.entries(customer.attributes).map(([key, value]) => (
                <div key={key} className="flex justify-between rounded-md bg-muted/50 px-3 py-2">
                  <dt className="text-muted-foreground">{key}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">スコア履歴</CardTitle>
        </CardHeader>
        <CardContent>
          {scoresLoading ? (
            <div className="h-32 animate-pulse rounded bg-muted" />
          ) : scores && scores.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>スコア</TableHead>
                  <TableHead>ティア</TableHead>
                  <TableHead>ルールセット</TableHead>
                  <TableHead>バージョン</TableHead>
                  <TableHead>評価日時</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {scores.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-bold">{s.score.toFixed(1)}</TableCell>
                    <TableCell>
                      <Badge variant={TIER_VARIANT[s.tier]}>{TIER_LABELS[s.tier]}</Badge>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{s.rule_set_id}</TableCell>
                    <TableCell>v{s.rule_set_version}</TableCell>
                    <TableCell>{formatDateTime(s.scored_at)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="py-8 text-center text-sm text-muted-foreground">スコア履歴がありません</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
