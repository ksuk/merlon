import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type AlertSeverity, type AlertStatus } from "@/lib/api"
import { ArrowLeft } from "lucide-react"
import { useCallback, useState } from "react"
import { Link, useParams } from "react-router-dom"

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

const STATUS_TRANSITIONS: Record<AlertStatus, { label: string; value: AlertStatus }[]> = {
  open: [
    { label: "調査開始", value: "investigating" },
    { label: "エスカレーション", value: "escalated" },
  ],
  investigating: [
    { label: "エスカレーション", value: "escalated" },
    { label: "真陽性として完了", value: "closed_true_positive" },
    { label: "偽陽性として完了", value: "closed_false_positive" },
  ],
  escalated: [
    { label: "真陽性として完了", value: "closed_true_positive" },
    { label: "偽陽性として完了", value: "closed_false_positive" },
  ],
  closed_true_positive: [],
  closed_false_positive: [],
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function AlertDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { data: alert, loading, error } = useApi(
    useCallback(() => api.alerts.get(id!), [id]),
  )
  const [updating, setUpdating] = useState(false)

  async function handleStatusChange(status: AlertStatus) {
    if (!id) return
    setUpdating(true)
    try {
      await api.alerts.updateStatus(id, status)
      window.location.reload()
    } catch {
      setUpdating(false)
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

  if (error || !alert) {
    return (
      <div className="space-y-4">
        <Link to="/alerts" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> アラート一覧に戻る
        </Link>
        <p className="text-destructive">アラートデータの取得に失敗しました</p>
      </div>
    )
  }

  const transitions = STATUS_TRANSITIONS[alert.status] ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/alerts" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> 戻る
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">アラート詳細</h1>
        <Badge variant={SEVERITY_VARIANT[alert.severity]}>
          {SEVERITY_LABELS[alert.severity]}
        </Badge>
        <Badge variant="outline">{STATUS_LABELS[alert.status]}</Badge>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">アラート情報</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">ID</dt>
                <dd className="font-mono">{alert.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">顧客ID</dt>
                <dd>
                  <Link to={`/customers/${alert.customer_id}`} className="font-mono text-primary underline-offset-4 hover:underline">
                    {alert.customer_id}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">シナリオ</dt>
                <dd className="font-mono">{alert.scenario_id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">スコア</dt>
                <dd className="text-lg font-bold">{alert.score.toFixed(1)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">検出日時</dt>
                <dd>{formatDateTime(alert.detected_at)}</dd>
              </div>
              {alert.resolved_at && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">解決日時</dt>
                  <dd>{formatDateTime(alert.resolved_at)}</dd>
                </div>
              )}
              {alert.resolved_by && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">解決者</dt>
                  <dd>{alert.resolved_by}</dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">説明</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{alert.description}</p>
            {alert.transaction_ids.length > 0 && (
              <div className="mt-4">
                <p className="mb-2 text-xs font-medium text-muted-foreground">関連取引</p>
                <div className="flex flex-wrap gap-1">
                  {alert.transaction_ids.map((tid) => (
                    <Badge key={tid} variant="secondary" className="font-mono text-xs">
                      {tid}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {transitions.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">ステータス変更</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex gap-2">
              {transitions.map((t) => (
                <Button
                  key={t.value}
                  variant={t.value.startsWith("closed") ? "destructive" : "outline"}
                  size="sm"
                  disabled={updating}
                  onClick={() => handleStatusChange(t.value)}
                >
                  {t.label}
                </Button>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
