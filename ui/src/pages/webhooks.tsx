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
import { api, type WebhookDLQEntry, type WebhookDelivery, type WebhookEventType } from "@/lib/api"
import { ChevronDown, ChevronUp, Plus, Trash2 } from "lucide-react"
import { useEffect, useRef, useState } from "react"

const ALL_EVENTS: { value: WebhookEventType; label: string }[] = [
  { value: "alert.created", label: "アラート作成" },
  { value: "alert.resolved", label: "アラート解決" },
  { value: "case.created", label: "ケース作成" },
  { value: "case.updated", label: "ケース更新" },
  { value: "case.closed", label: "ケースクローズ" },
  { value: "str.created", label: "STR作成" },
  { value: "score.changed", label: "スコア変更" },
  { value: "screening.match", label: "スクリーニング一致" },
  { value: "screening_true_positive", label: "スクリーニング真陽性" },
  { value: "edd_required", label: "EDD要求" },
  { value: "transaction_restriction_recommended", label: "取引制限推奨" },
  { value: "relationship_decline_recommended", label: "取引謝絶推奨" },
]

function eventLabel(event: WebhookEventType) {
  return ALL_EVENTS.find((ae) => ae.value === event)?.label ?? event
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function WebhooksPage() {
  const { data: webhooks, loading, error } = useApi(api.webhooks.list)
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const urlRef = useRef<HTMLInputElement>(null)
  const [selectedEvents, setSelectedEvents] = useState<WebhookEventType[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([])
  const [loadingDeliveries, setLoadingDeliveries] = useState(false)

  // DLQ タブ（api.md §3.1「DLQ内イベントの再処理はUI上から実行可能」）。
  const [tab, setTab] = useState<"webhooks" | "dlq">("webhooks")
  const [dlqEntries, setDlqEntries] = useState<WebhookDLQEntry[] | null>(null)
  const [loadingDlq, setLoadingDlq] = useState(false)
  const [reprocessingId, setReprocessingId] = useState<string | null>(null)

  async function loadDlq() {
    setLoadingDlq(true)
    try {
      const entries = await api.webhooks.listDLQ()
      setDlqEntries(entries)
    } finally {
      setLoadingDlq(false)
    }
  }

  useEffect(() => {
    if (tab === "dlq") {
      loadDlq()
    }
  }, [tab])

  async function handleReprocess(id: string) {
    setReprocessingId(id)
    try {
      await api.webhooks.reprocessDLQ(id)
      await loadDlq()
    } finally {
      setReprocessingId(null)
    }
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const url = urlRef.current?.value.trim()
    if (!url || selectedEvents.length === 0) return
    setCreating(true)
    try {
      await api.webhooks.create(url, selectedEvents)
      window.location.reload()
    } catch {
      setCreating(false)
    }
  }

  async function handleDelete(id: string) {
    await api.webhooks.delete(id)
    window.location.reload()
  }

  function toggleEvent(event: WebhookEventType) {
    setSelectedEvents((prev) =>
      prev.includes(event) ? prev.filter((e) => e !== event) : [...prev, event],
    )
  }

  async function toggleDeliveries(id: string) {
    if (expandedId === id) {
      setExpandedId(null)
      return
    }
    setExpandedId(id)
    setLoadingDeliveries(true)
    try {
      const data = await api.webhooks.deliveries(id)
      setDeliveries(data)
    } finally {
      setLoadingDeliveries(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-40 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">Webhookデータの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">Webhook管理</h1>
        {tab === "webhooks" && (
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            新規作成
          </Button>
        )}
      </div>

      <div className="flex gap-2 border-b">
        <button
          type="button"
          onClick={() => setTab("webhooks")}
          className={`border-b-2 px-3 py-2 text-sm font-medium ${
            tab === "webhooks" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
          }`}
        >
          Webhook
        </button>
        <button
          type="button"
          onClick={() => setTab("dlq")}
          className={`border-b-2 px-3 py-2 text-sm font-medium ${
            tab === "dlq" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
          }`}
        >
          DLQ
        </button>
      </div>

      {tab === "dlq" ? (
        loadingDlq ? (
          <div className="h-48 animate-pulse rounded-xl border bg-muted" />
        ) : dlqEntries && dlqEntries.length > 0 ? (
          <div className="rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>イベント</TableHead>
                  <TableHead>試行回数</TableHead>
                  <TableHead>最終エラー</TableHead>
                  <TableHead>失敗日時</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {dlqEntries.map((entry) => (
                  <TableRow key={entry.id}>
                    <TableCell className="text-xs">{eventLabel(entry.event)}</TableCell>
                    <TableCell>{entry.attempt_count}</TableCell>
                    <TableCell className="max-w-[300px] truncate text-xs text-destructive">
                      {entry.last_error ?? "-"}
                    </TableCell>
                    <TableCell className="text-xs">{formatDateTime(entry.failed_at)}</TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={reprocessingId === entry.id}
                        onClick={() => handleReprocess(entry.id)}
                      >
                        再処理
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              DLQに退避されたイベントはありません
            </CardContent>
          </Card>
        )
      ) : (
        <>
          {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Webhook作成</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">URL</label>
                <input
                  ref={urlRef}
                  type="url"
                  required
                  placeholder="https://example.com/webhook"
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium">イベント</label>
                <div className="flex flex-wrap gap-2">
                  {ALL_EVENTS.map((evt) => (
                    <button
                      key={evt.value}
                      type="button"
                      onClick={() => toggleEvent(evt.value)}
                      className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                        selectedEvents.includes(evt.value)
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-input text-muted-foreground hover:bg-accent"
                      }`}
                    >
                      {evt.label}
                    </button>
                  ))}
                </div>
              </div>
              <Button type="submit" size="sm" disabled={creating || selectedEvents.length === 0}>
                作成
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        {webhooks && webhooks.length > 0 ? (
          webhooks.map((w) => (
            <Card key={w.id}>
              <CardContent className="p-4">
                <div className="flex items-center justify-between">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-sm">{w.url}</span>
                      <Badge variant={w.active ? "low" : "secondary"}>
                        {w.active ? "有効" : "無効"}
                      </Badge>
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {w.events.map((e) => (
                        <Badge key={e} variant="outline" className="text-xs">
                          {eventLabel(e)}
                        </Badge>
                      ))}
                    </div>
                    <p className="text-xs text-muted-foreground">
                      作成: {formatDateTime(w.created_at)}
                    </p>
                  </div>
                  <div className="flex gap-1">
                    <Button variant="ghost" size="sm" onClick={() => toggleDeliveries(w.id)}>
                      {expandedId === w.id ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                      配信履歴
                    </Button>
                    <Button variant="ghost" size="icon" onClick={() => handleDelete(w.id)}>
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </div>
                {expandedId === w.id && (
                  <div className="mt-3 border-t pt-3">
                    {loadingDeliveries ? (
                      <div className="h-16 animate-pulse rounded bg-muted" />
                    ) : deliveries.length > 0 ? (
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>イベント</TableHead>
                            <TableHead>ステータス</TableHead>
                            <TableHead>結果</TableHead>
                            <TableHead>日時</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {deliveries.map((d) => (
                            <TableRow key={d.id}>
                              <TableCell className="text-xs">
                                {eventLabel(d.event)}
                              </TableCell>
                              <TableCell>{d.status_code || "-"}</TableCell>
                              <TableCell>
                                <Badge variant={d.success ? "low" : "destructive"}>
                                  {d.success ? "成功" : "失敗"}
                                </Badge>
                                {d.error && <span className="ml-2 text-xs text-destructive">{d.error}</span>}
                              </TableCell>
                              <TableCell className="text-xs">{formatDateTime(d.created_at)}</TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    ) : (
                      <p className="py-4 text-center text-sm text-muted-foreground">配信履歴がありません</p>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
          ))
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              Webhookが登録されていません
            </CardContent>
          </Card>
        )}
          </div>
        </>
      )}
    </div>
  )
}
