import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type CasePriority, type CaseStatus } from "@/lib/api"
import { ArrowLeft, Send } from "lucide-react"
import { useCallback, useRef, useState } from "react"
import { Link, useParams } from "react-router-dom"

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high"> = {
  low: "low",
  medium: "medium",
  high: "high",
}

const PRIORITY_LABELS: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
}

const STATUS_LABELS: Record<CaseStatus, string> = {
  open: "未対応",
  investigating: "調査中",
  escalated: "エスカレーション",
  closed: "完了",
}

const STATUS_TRANSITIONS: Record<CaseStatus, { label: string; value: CaseStatus }[]> = {
  open: [
    { label: "調査開始", value: "investigating" },
    { label: "エスカレーション", value: "escalated" },
  ],
  investigating: [
    { label: "エスカレーション", value: "escalated" },
    { label: "クローズ", value: "closed" },
  ],
  escalated: [{ label: "クローズ", value: "closed" }],
  closed: [],
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function CaseDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { data: caseData, loading, error } = useApi(
    useCallback(() => api.cases.get(id!), [id]),
  )
  const [updating, setUpdating] = useState(false)
  const [addingNote, setAddingNote] = useState(false)
  const noteRef = useRef<HTMLTextAreaElement>(null)

  async function handleStatusChange(status: CaseStatus) {
    if (!id) return
    setUpdating(true)
    try {
      await api.cases.update(id, { status })
      window.location.reload()
    } catch {
      setUpdating(false)
    }
  }

  async function handleAddNote(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !noteRef.current?.value.trim()) return
    setAddingNote(true)
    try {
      await api.cases.addNote(id, "operator", noteRef.current.value.trim())
      window.location.reload()
    } catch {
      setAddingNote(false)
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

  if (error || !caseData) {
    return (
      <div className="space-y-4">
        <Link to="/cases" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> ケース一覧に戻る
        </Link>
        <p className="text-destructive">ケースデータの取得に失敗しました</p>
      </div>
    )
  }

  const transitions = STATUS_TRANSITIONS[caseData.status] ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/cases" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> 戻る
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">ケース詳細</h1>
        <Badge variant={PRIORITY_VARIANT[caseData.priority]}>
          {PRIORITY_LABELS[caseData.priority]}
        </Badge>
        <Badge variant="outline">{STATUS_LABELS[caseData.status]}</Badge>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">ケース情報</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">ID</dt>
                <dd className="font-mono">{caseData.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">顧客ID</dt>
                <dd>
                  <Link to={`/customers/${caseData.customer_id}`} className="font-mono text-primary underline-offset-4 hover:underline">
                    {caseData.customer_id}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">担当者</dt>
                <dd>{caseData.assigned_to || "-"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">作成日時</dt>
                <dd>{formatDateTime(caseData.created_at)}</dd>
              </div>
              {caseData.closed_at && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">クローズ日時</dt>
                  <dd>{formatDateTime(caseData.closed_at)}</dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">概要</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{caseData.summary}</p>
            {caseData.alert_ids.length > 0 && (
              <div className="mt-4">
                <p className="mb-2 text-xs font-medium text-muted-foreground">関連アラート</p>
                <div className="flex flex-wrap gap-1">
                  {caseData.alert_ids.map((aid) => (
                    <Link key={aid} to={`/alerts/${aid}`}>
                      <Badge variant="secondary" className="font-mono text-xs hover:bg-secondary/80">
                        {aid}
                      </Badge>
                    </Link>
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
                  variant={t.value === "closed" ? "destructive" : "outline"}
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

      <Card>
        <CardHeader>
          <CardTitle className="text-base">ノート</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {caseData.notes && caseData.notes.length > 0 ? (
            <div className="space-y-3">
              {caseData.notes.map((note) => (
                <div key={note.id} className="rounded-lg bg-muted/50 p-3">
                  <div className="mb-1 flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="font-medium">{note.author}</span>
                    <span>{formatDateTime(note.created_at)}</span>
                  </div>
                  <p className="text-sm">{note.content}</p>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">ノートがありません</p>
          )}

          {caseData.status !== "closed" && (
            <form onSubmit={handleAddNote} className="flex gap-2">
              <textarea
                ref={noteRef}
                placeholder="ノートを追加..."
                className="flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                rows={2}
              />
              <Button type="submit" size="sm" disabled={addingNote}>
                <Send className="h-4 w-4" />
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
