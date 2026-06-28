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
import { api, type RiskTier } from "@/lib/api"

const TIER_VARIANT: Record<RiskTier, "low" | "medium" | "high"> = {
  low: "low",
  medium: "medium",
  high: "high",
}

const TIER_LABELS: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
}

const TYPE_LABELS: Record<string, string> = {
  individual: "個人",
  corporate_domestic: "国内法人",
  corporate_foreign: "海外法人",
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString("ja-JP")
}

export function CustomersPage() {
  const { data: customers, loading, error } = useApi(api.customers.list)

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">顧客データの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">顧客一覧</h1>
        <p className="text-sm text-muted-foreground">{customers?.length ?? 0} 件</p>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>外部ID</TableHead>
              <TableHead>種別</TableHead>
              <TableHead>国</TableHead>
              <TableHead>リスクスコア</TableHead>
              <TableHead>リスクティア</TableHead>
              <TableHead>最終スコアリング</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {customers && customers.length > 0 ? (
              customers.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="font-mono text-sm">{c.external_id}</TableCell>
                  <TableCell>{TYPE_LABELS[c.customer_type] ?? c.customer_type}</TableCell>
                  <TableCell>{c.country_code}</TableCell>
                  <TableCell>
                    {c.risk_score != null ? c.risk_score.toFixed(1) : "-"}
                  </TableCell>
                  <TableCell>
                    {c.risk_tier ? (
                      <Badge variant={TIER_VARIANT[c.risk_tier]}>
                        {TIER_LABELS[c.risk_tier] ?? c.risk_tier}
                      </Badge>
                    ) : (
                      <Badge variant="secondary">未スコア</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {c.last_scored_at ? formatDate(c.last_scored_at) : "-"}
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  顧客データがありません
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
