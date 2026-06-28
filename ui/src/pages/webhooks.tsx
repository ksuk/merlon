import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type WebhookEventType } from "@/lib/api"
import { Plus, Trash2 } from "lucide-react"
import { useRef, useState } from "react"

const ALL_EVENTS: { value: WebhookEventType; label: string }[] = [
  { value: "alert.created", label: "アラート作成" },
  { value: "alert.resolved", label: "アラート解決" },
  { value: "case.created", label: "ケース作成" },
  { value: "case.updated", label: "ケース更新" },
  { value: "case.closed", label: "ケースクローズ" },
  { value: "str.created", label: "STR作成" },
  { value: "score.changed", label: "スコア変更" },
  { value: "screening.match", label: "スクリーニング一致" },
]

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function WebhooksPage() {
  const { data: webhooks, loading, error } = useApi(api.webhooks.list)
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const urlRef = useRef<HTMLInputElement>(null)
  const [selectedEvents, setSelectedEvents] = useState<WebhookEventType[]>([])

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
        <Button size="sm" onClick={() => setShowForm(!showForm)}>
          <Plus className="h-4 w-4" />
          新規作成
        </Button>
      </div>

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
              <CardContent className="flex items-center justify-between p-4">
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
                        {ALL_EVENTS.find((ae) => ae.value === e)?.label ?? e}
                      </Badge>
                    ))}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    作成: {formatDateTime(w.created_at)}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => handleDelete(w.id)}
                >
                  <Trash2 className="h-4 w-4 text-destructive" />
                </Button>
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
    </div>
  )
}
