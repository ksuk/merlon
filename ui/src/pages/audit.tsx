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
import { useTranslation } from "react-i18next"

// ACTION_CATEGORY_VALUES mirror domain.ResourceTypesForCategory's map keys
// (the audit design §1 operation category, ALD-001's action_category filter axis)
// and are sent verbatim as the API query parameter, so they must stay in
// Japanese to match the backend contract; only the displayed label (via
// ACTION_CATEGORY_LABEL_KEYS + t()) is translated.
const ACTION_CATEGORY_VALUES = ["", "認証", "顧客データ", "ルール管理", "アラート・ケース", "STR", "ホワイトリスト", "管理操作"] as const // i18n-ignore
const ACTION_CATEGORY_LABEL_KEYS: Record<string, string> = {
  "": "all",
  "認証": "auth", // i18n-ignore
  "顧客データ": "customerData", // i18n-ignore
  "ルール管理": "ruleManagement", // i18n-ignore
  "アラート・ケース": "alertCase", // i18n-ignore
  STR: "str",
  "ホワイトリスト": "whitelist", // i18n-ignore
  "管理操作": "adminOp", // i18n-ignore
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function AuditPage() {
  const { t, i18n } = useTranslation()
  const actionLabels: Record<string, string> = {
    create: t("audit.action.create"),
    update: t("audit.action.update"),
    update_status: t("audit.action.update_status"),
    delete: t("audit.action.delete"),
    score_customer: t("audit.action.score_customer"),
    screen_customer: t("audit.action.screen_customer"),
    run_backtest: t("audit.action.run_backtest"),
    create_str: t("audit.action.create_str"),
    export_audit_logs: t("audit.action.export_audit_logs"),
  }
  const resourceLabels: Record<string, string> = {
    customers: t("audit.resource.customers"),
    transactions: t("audit.resource.transactions"),
    alerts: t("audit.resource.alerts"),
    cases: t("audit.resource.cases"),
    webhooks: t("audit.resource.webhooks"),
    batch: t("audit.resource.batch"),
    reports: t("audit.resource.reports"),
    admin: t("audit.resource.admin"),
    rules: t("audit.resource.rules"),
    audit: t("audit.resource.audit"),
  }

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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload when audit filters change
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
    return <p className="p-12 text-center text-destructive">{t("audit.error")}</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("audit.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("audit.count", { count: entries?.length ?? 0 })}</p>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div>
          <label htmlFor="audit-since" className="mb-1 block text-xs font-medium text-muted-foreground">
            {t("audit.filter.since")}
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
            {t("audit.filter.until")}
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
            {t("audit.filter.category")}
          </label>
          <select
            id="audit-category"
            value={actionCategory}
            onChange={(e) => setActionCategory(e.target.value)}
            className="rounded-md border bg-background px-2 py-1 text-sm"
          >
            {ACTION_CATEGORY_VALUES.map((value) => (
              <option key={value} value={value}>
                {t(`audit.category.${ACTION_CATEGORY_LABEL_KEYS[value]}`)}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="audit-user" className="mb-1 block text-xs font-medium text-muted-foreground">
            {t("audit.filter.user")}
          </label>
          <input
            id="audit-user"
            type="text"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            placeholder={t("audit.filter.userPlaceholder")}
            className="rounded-md border bg-background px-2 py-1 text-sm"
          />
        </div>
        <div>
          <label htmlFor="audit-resource" className="mb-1 block text-xs font-medium text-muted-foreground">
            {t("audit.filter.resourceId")}
          </label>
          <input
            id="audit-resource"
            type="text"
            value={resourceId}
            onChange={(e) => setResourceId(e.target.value)}
            placeholder={t("audit.filter.resourceIdPlaceholder")}
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
              <TableHead>{t("audit.table.header.timestamp")}</TableHead>
              <TableHead>{t("audit.table.header.action")}</TableHead>
              <TableHead>{t("audit.table.header.resource")}</TableHead>
              <TableHead>{t("audit.table.header.resourceId")}</TableHead>
              <TableHead>{t("audit.table.header.user")}</TableHead>
              <TableHead>{t("audit.table.header.ipAddress")}</TableHead>
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
                        {formatDateTime(e.created_at, i18n.language)}
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {actionLabels[e.action] ?? e.action}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {resourceLabels[e.resource_type] ?? e.resource_type}
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
                  {t("audit.table.empty")}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {hasMore && (
        <div className="flex justify-center">
          <Button variant="outline" size="sm" disabled={loading} onClick={() => load(nextCursor)}>
            {t("audit.loadMore")}
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
