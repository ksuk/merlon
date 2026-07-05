import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type CasePriority, type CaseStatus, type Customer, type RelatedCase } from "@/lib/api"
import { ArrowLeft, Send } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { Link, useParams } from "react-router-dom"

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

const PRIORITY_LABELS: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
  critical: "重大",
}

// new/reopened/str_filed は WS-8 Task 1 で追加。open は new のエイリアス。
const STATUS_LABELS: Record<CaseStatus, string> = {
  open: "新規",
  new: "新規",
  investigating: "調査中",
  escalated: "エスカレーション",
  closed: "完了",
  reopened: "再オープン",
  str_filed: "STR対象",
}

// case-management.md §ケースのステータス遷移の遷移図どおり
// （NEW→INVESTIGATING→{ESCALATED→INVESTIGATING(差し戻し), CLOSED, STR_FILED}）。
// CLOSED→REOPENED は理由必須・Analyst以上のため、別途の再オープンフォームで扱う
// （このテーブルには含めない）。
const STATUS_TRANSITIONS: Record<CaseStatus, { label: string; value: CaseStatus }[]> = {
  open: [{ label: "調査開始", value: "investigating" }],
  new: [{ label: "調査開始", value: "investigating" }],
  investigating: [
    { label: "エスカレーション", value: "escalated" },
    { label: "クローズ", value: "closed" },
    { label: "STR対象として届出", value: "str_filed" },
  ],
  escalated: [{ label: "差し戻し（調査中に戻す）", value: "investigating" }],
  closed: [],
  reopened: [{ label: "調査再開", value: "investigating" }],
  str_filed: [],
}

const EDD_STAGE_LABELS: { key: keyof Customer; label: string; variant: "medium" | "high" | "critical" }[] = [
  { key: "edd_stage3_notified_at", label: "ステージ3: 取引謝絶推奨済み", variant: "critical" },
  { key: "edd_stage2_notified_at", label: "ステージ2: 取引制限推奨済み", variant: "high" },
  { key: "edd_stage1_last_sent_at", label: "ステージ1: リマインダー送信済み", variant: "medium" },
]

// eddStageDisplay picks the highest-reached EDD escalation stage for
// display (case-management.md §EDD未実施継続時の段階的措置). Returns null
// when the customer has no open EDD requirement.
function eddStageDisplay(customer: Customer | null) {
  if (!customer?.edd_requested_at) return null
  for (const stage of EDD_STAGE_LABELS) {
    if (customer[stage.key]) return stage
  }
  return { key: "edd_requested_at" as const, label: "EDD要求中（エスカレーション前）", variant: "medium" as const }
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

  // ケース間の関連付け（case-management.md §ケース間の関連付け）。
  const [relatedCases, setRelatedCases] = useState<RelatedCase[] | null>(null)

  // EDD段階表示（case-management.md §EDD未実施継続時の段階的措置）。
  const [customer, setCustomer] = useState<Customer | null>(null)

  // 再オープン（理由必須・Analyst以上、case-management.md「再オープン時は
  // 理由（テキスト、必須）を記録する」）。
  const [reopenReason, setReopenReason] = useState("")
  const [reopening, setReopening] = useState(false)
  const [reopenError, setReopenError] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    api.cases.related(id).then(setRelatedCases).catch(() => setRelatedCases([]))
  }, [id])

  useEffect(() => {
    if (!caseData?.customer_id) return
    api.customers.get(caseData.customer_id).then(setCustomer).catch(() => setCustomer(null))
  }, [caseData?.customer_id])

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

  async function handleReopen(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !reopenReason.trim()) return
    setReopening(true)
    setReopenError(null)
    try {
      await api.cases.update(id, { status: "reopened", reason: reopenReason.trim() })
      window.location.reload()
    } catch (err) {
      setReopenError(err instanceof Error ? err.message : String(err))
      setReopening(false)
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
  const eddStage = eddStageDisplay(customer)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-4">
        <Link to="/cases" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> 戻る
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">ケース詳細</h1>
        <Badge variant={PRIORITY_VARIANT[caseData.priority]}>
          {PRIORITY_LABELS[caseData.priority]}
        </Badge>
        <Badge variant="outline">{STATUS_LABELS[caseData.status]}</Badge>
        {eddStage && <Badge variant={eddStage.variant}>{eddStage.label}</Badge>}
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

      {caseData.status === "closed" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">再オープン</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleReopen} className="flex gap-2">
              <input
                type="text"
                value={reopenReason}
                onChange={(e) => setReopenReason(e.target.value)}
                placeholder="再オープンの理由（必須）"
                className="flex-1 rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
              <Button type="submit" size="sm" disabled={reopening || !reopenReason.trim()}>
                再オープン
              </Button>
            </form>
            {reopenError && <p className="mt-2 text-sm text-destructive">{reopenError}</p>}
          </CardContent>
        </Card>
      )}

      {caseData.reopen_reason && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">再オープン理由</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{caseData.reopen_reason}</p>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">関連ケース</CardTitle>
        </CardHeader>
        <CardContent>
          {relatedCases && relatedCases.length > 0 ? (
            <div className="space-y-2">
              {relatedCases.map((rc) => (
                <div key={rc.case.id} className="flex items-center justify-between rounded-lg bg-muted/50 p-3 text-sm">
                  <Link to={`/cases/${rc.case.id}`} className="font-mono text-primary hover:underline">
                    {rc.case.id}
                  </Link>
                  <div className="flex items-center gap-2">
                    <Badge variant="outline">{STATUS_LABELS[rc.case.status]}</Badge>
                    <Badge variant="secondary" className="text-xs">
                      {rc.link_type === "auto" ? "自動抽出（同一顧客）" : "手動リンク"}
                    </Badge>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">関連ケースはありません</p>
          )}
        </CardContent>
      </Card>

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
