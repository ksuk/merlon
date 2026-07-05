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
import { RuleDiffView } from "@/components/audit/rule-diff-view"
import { api, type AuditEntry, type AuditListParams } from "@/lib/api"
import { Download } from "lucide-react"
import { Fragment, useEffect, useState } from "react"

const ACTION_LABELS: Record<string, string> = {
  create: "作成",
  update: "更新",
  update_status: "ステータス変更",
  delete: "削除",
  score_customer: "スコアリング",
  screen_customer: "スクリーニング",
  run_backtest: "バックテスト",
  create_str: "STR作成",
  export_audit_logs: "監査ログエクスポート",
}

const RESOURCE_LABELS: Record<string, string> = {
  customers: "顧客",
  transactions: "取引",
  alerts: "アラート",
  cases: "ケース",
  webhooks: "Webhook",
  batch: "バッチ",
  reports: "レポート",
  admin: "管理",
  rules: "ルール",
  audit: "監査ログ",
}

// ACTION_CATEGORIES mirrors domain.ResourceTypesForCategory's keys
// (audit.md §1 操作カテゴリ, ALD-001's action_category filter axis).
const ACTION_CATEGORIES = [
  { value: "", label: "全カテゴリ" },
  { value: "認証", label: "認証" },
  { value: "顧客データ", label: "顧客データ" },
  { value: "ルール管理", label: "ルール管理" },
  { value: "アラート・ケース", label: "アラート・ケース" },
  { value: "STR", label: "STR" },
  { value: "ホワイトリスト", label: "ホワイトリスト" },
  { value: "管理操作", label: "管理操作" },
]

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function AuditPage() {
  const [entries, setEntries] = useState<AuditEntry[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [nextCursor, setNextCursor] = useState<string | undefined>()

  const [since, setSince] = useState("")
  const [until, setUntil] = useState("")
  const [userId, setUserId] = useState("")
  const [resourceId, setResourceId] = useState("")
  const [actionCategory, setActionCategory] = useState("")

  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState<string | null>(null)

  function currentFilters(): AuditListParams {
    return {
      since: since ? new Date(since).toISOString() : undefined,
      until: until ? new Date(until).toISOString() : undefined,
      userId: userId || undefined,
      resourceId: resourceId || undefined,
      actionCategory: actionCategory || undefined,
    }
  }

  async function load(cursor?: string) {
    setLoading(true)
    setError(null)
    try {
      const res = await api.audit.list({ ...currentFilters(), cursor, limit: 50 })
      setEntries((prev) => (cursor ? [...(prev ?? []), ...res.data] : res.data))
      setHasMore(res.pagination.has_more)
      setNextCursor(res.pagination.next_cursor)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [since, until, userId, resourceId, actionCategory])

  async function handleExport(format: "csv" | "json") {
    setExporting(true)
    setExportError(null)
    try {
      await api.audit.export(currentFilters(), format)
    } catch (err) {
      setExportError(err instanceof Error ? err.message : String(err))
    } finally {
      setExporting(false)
    }
  }

  if (loading && !entries) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">監査ログの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">監査ログ</h1>
        <p className="text-sm text-muted-foreground">{entries?.length ?? 0} 件</p>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div>
          <label htmlFor="audit-since" className="mb-1 block text-xs font-medium text-muted-foreground">
            期間（開始）
          </label>
          <input
            id="audit-since"
            type="datetime-local"
            value={since}
            onChange={(e) => setSince(e.target.value)}
            className="rounded-md border bg-background px-2 py-1 text-sm"
          />
        </div>
        <div>
          <label htmlFor="audit-until" className="mb-1 block text-xs font-medium text-muted-foreground">
            期間（終了）
          </label>
          <input
            id="audit-until"
            type="datetime-local"
            value={until}
            onChange={(e) => setUntil(e.target.value)}
            className="rounded-md border bg-background px-2 py-1 text-sm"
          />
        </div>
        <div>
          <label htmlFor="audit-category" className="mb-1 block text-xs font-medium text-muted-foreground">
            操作カテゴリ
          </label>
          <select
            id="audit-category"
            value={actionCategory}
            onChange={(e) => setActionCategory(e.target.value)}
            className="rounded-md border bg-background px-2 py-1 text-sm"
          >
            {ACTION_CATEGORIES.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="audit-user" className="mb-1 block text-xs font-medium text-muted-foreground">
            操作者
          </label>
          <input
            id="audit-user"
            type="text"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            placeholder="user_id"
            className="rounded-md border bg-background px-2 py-1 text-sm"
          />
        </div>
        <div>
          <label htmlFor="audit-resource" className="mb-1 block text-xs font-medium text-muted-foreground">
            対象リソースID
          </label>
          <input
            id="audit-resource"
            type="text"
            value={resourceId}
            onChange={(e) => setResourceId(e.target.value)}
            placeholder="resource_id"
            className="rounded-md border bg-background px-2 py-1 text-sm"
          />
        </div>
        <div className="ml-auto flex items-center gap-2">
          {exportError && <p className="text-xs text-destructive">{exportError}</p>}
          <Button variant="outline" size="sm" disabled={exporting} onClick={() => handleExport("csv")}>
            <Download className="h-4 w-4" />
            CSV
          </Button>
          <Button variant="outline" size="sm" disabled={exporting} onClick={() => handleExport("json")}>
            <Download className="h-4 w-4" />
            JSON
          </Button>
        </div>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>日時</TableHead>
              <TableHead>操作</TableHead>
              <TableHead>リソース</TableHead>
              <TableHead>リソースID</TableHead>
              <TableHead>ユーザー</TableHead>
              <TableHead>IPアドレス</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries && entries.length > 0 ? (
              entries.map((e) => {
                const hasDiff = Boolean(e.details?.diff)
                const isExpanded = expandedId === e.id
                return (
                  <Fragment key={e.id}>
                    <TableRow
                      onClick={hasDiff ? () => setExpandedId(isExpanded ? null : e.id) : undefined}
                      className={hasDiff ? "cursor-pointer hover:bg-accent/50" : undefined}
                    >
                      <TableCell className="whitespace-nowrap text-sm">
                        {formatDateTime(e.created_at)}
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {ACTION_LABELS[e.action] ?? e.action}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {RESOURCE_LABELS[e.resource_type] ?? e.resource_type}
                      </TableCell>
                      <TableCell className="font-mono text-sm">
                        {e.resource_id || "-"}
                      </TableCell>
                      <TableCell>{e.user_id || "-"}</TableCell>
                      <TableCell className="font-mono text-sm">
                        {e.ip_address || "-"}
                      </TableCell>
                    </TableRow>
                    {hasDiff && isExpanded && (
                      <TableRow>
                        <TableCell colSpan={6} className="bg-muted/30 p-4">
                          <RuleDiffView details={e.details} />
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                )
              })
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  監査ログがありません
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {hasMore && (
        <div className="flex justify-center">
          <Button variant="outline" size="sm" disabled={loading} onClick={() => load(nextCursor)}>
            さらに読み込む
          </Button>
        </div>
      )}
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
