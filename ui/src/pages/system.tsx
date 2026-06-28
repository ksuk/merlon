import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
import { CheckCircle, XCircle } from "lucide-react"

const FEATURE_LABELS: Record<string, string> = {
  auth: "API認証",
  audit: "監査ログ",
  cases: "ケース管理",
  webhooks: "Webhook",
  rate_limit: "レート制限",
  scoring: "CDDスコアリング",
  monitoring: "取引モニタリング",
  screening: "スクリーニング",
  backtest: "バックテスト",
  config: "設定検証",
}

const COMPONENT_LABELS: Record<string, string> = {
  api: "Go API",
  engine: "Rust Engine",
  database: "PostgreSQL",
}

export function SystemPage() {
  const { data: info, loading, error } = useApi(api.system.info)

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="grid gap-4 md:grid-cols-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <div key={i} className="h-48 animate-pulse rounded-xl border bg-muted" />
          ))}
        </div>
      </div>
    )
  }

  if (error || !info) {
    return <p className="p-12 text-center text-destructive">システム情報の取得に失敗しました</p>
  }

  const enabledCount = Object.values(info.features).filter(Boolean).length
  const totalCount = Object.keys(info.features).length

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">システム情報</h1>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">バージョン</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">v{info.version}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">APIエンドポイント</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{info.endpoints}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">有効機能</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{enabledCount}/{totalCount}</p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">コンポーネント</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {info.components.map((c) => (
                <div key={c} className="flex items-center gap-2">
                  <CheckCircle className="h-4 w-4 text-green-600" />
                  <span className="text-sm">{COMPONENT_LABELS[c] ?? c}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">機能ステータス</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {Object.entries(info.features).map(([key, enabled]) => (
                <div key={key} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    {enabled ? (
                      <CheckCircle className="h-4 w-4 text-green-600" />
                    ) : (
                      <XCircle className="h-4 w-4 text-muted-foreground" />
                    )}
                    <span className="text-sm">{FEATURE_LABELS[key] ?? key}</span>
                  </div>
                  <Badge variant={enabled ? "low" : "secondary"}>
                    {enabled ? "有効" : "無効"}
                  </Badge>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
