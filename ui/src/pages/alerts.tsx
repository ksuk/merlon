import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, type Alert, type AlertSeverity, type AlertStatus } from "@/lib/api"
import { useEffect, useState } from "react"
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
  const [alerts, setAlerts] = useState<Alert[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // 一括ケース統合（case-management.md §アラートの一括処理）: 選択済みアラート
  // の ID をそのまま bulk-case へ渡す（既存ケースに追加、または新規ケースと
  // してまとめる）。
  const [selected, setSelected] = useState<Set<string>>(new Set())

  // 一括クローズ（case-management.md §アラートの一括処理）: bulk-close は
  // シナリオID・期間・severity の「フィルタ条件」でアラートを絞り込んで
  // CLOSED にする API であり、個別のアラートID指定ではない。そのため UI も
  // チェックボックス選択とは独立したフィルタ入力フォームとする。
  const [closeScenarioId, setCloseScenarioId] = useState("")
  const [closeSeverity, setCloseSeverity] = useState<AlertSeverity | "">("")
  const [closeReason, setCloseReason] = useState("")

  async function reload() {
    setLoading(true)
    try {
      const data = await api.alerts.list()
      setAlerts(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    reload()
  }, [])

  function toggleSelected(id: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  async function handleBulkClose() {
    if (!closeReason.trim()) return
    setBusy(true)
    setActionError(null)
    try {
      await api.alerts.bulkClose({
        scenario_id: closeScenarioId.trim() || undefined,
        severity: closeSeverity || undefined,
        reason: closeReason.trim(),
      })
      setCloseScenarioId("")
      setCloseSeverity("")
      setCloseReason("")
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleBulkCase() {
    if (selected.size === 0) return
    const alertIds = [...selected]
    const customerId = alerts?.find((a) => a.id === alertIds[0])?.customer_id
    if (!customerId) return

    setBusy(true)
    setActionError(null)
    try {
      await api.alerts.bulkCase({ alert_ids: alertIds, customer_id: customerId })
      setSelected(new Set())
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

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

      <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-muted/40 p-4">
        <span className="text-sm font-medium">一括クローズ</span>
        <input
          type="text"
          placeholder="シナリオID（任意）"
          value={closeScenarioId}
          onChange={(e) => setCloseScenarioId(e.target.value)}
          className="h-9 w-40 rounded-md border border-input bg-background px-3 text-sm"
        />
        <select
          value={closeSeverity}
          onChange={(e) => setCloseSeverity(e.target.value as AlertSeverity | "")}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="">深刻度（任意）</option>
          <option value="low">低</option>
          <option value="medium">中</option>
          <option value="high">高</option>
          <option value="critical">重大</option>
        </select>
        <input
          type="text"
          placeholder="判断理由（必須）"
          value={closeReason}
          onChange={(e) => setCloseReason(e.target.value)}
          className="h-9 flex-1 min-w-[200px] rounded-md border border-input bg-background px-3 text-sm"
        />
        <Button
          size="sm"
          variant="destructive"
          disabled={busy || !closeReason.trim()}
          onClick={handleBulkClose}
        >
          条件に一致するアラートをクローズ
        </Button>
      </div>

      {selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-muted/40 p-4">
          <span className="text-sm font-medium">選択中: {selected.size} 件</span>
          <Button size="sm" variant="outline" disabled={busy} onClick={handleBulkCase}>
            選択したアラートをケースにまとめる
          </Button>
        </div>
      )}

      {actionError && <p className="text-sm text-destructive">{actionError}</p>}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10" />
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
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      aria-label={`select-${a.id}`}
                      checked={selected.has(a.id)}
                      onChange={() => toggleSelected(a.id)}
                    />
                  </TableCell>
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
                <TableCell colSpan={8} className="h-24 text-center text-muted-foreground">
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
