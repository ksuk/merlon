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
import { useApi } from "@/hooks/use-api"
import { api, type Alert, type AlertSeverity, type AlertStatus } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { compareRiskValues } from "@/lib/risk"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useSearchParams } from "react-router"

const SEVERITY_VARIANT: Record<AlertSeverity, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

function formatAge(iso: string, locale: string, label: (key: string, options?: Record<string, unknown>) => string, now: number) {
  const days = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 86400000))
  return label("alerts.queue.ageValue", { days, locale })
}

function slaState(dueAt: string | undefined, label: (key: string) => string, now: number) {
  if (!dueAt) return label("alerts.queue.slaUnassigned")
  return new Date(dueAt).getTime() < now ? label("alerts.queue.slaOverdue") : label("alerts.queue.slaOnTrack")
}

export function AlertsPage() {
  const { t, i18n } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  const [now, setNow] = useState(() => Date.now())
  const severityLabels: Record<string, string> = {
    low: t("alertSeverity.low"),
    medium: t("alertSeverity.medium"),
    high: t("alertSeverity.high"),
    critical: t("alertSeverity.critical"),
  }
  const statusLabels: Record<AlertStatus, string> = {
    open: t("alertStatus.open"),
    investigating: t("alertStatus.investigating"),
    escalated: t("alertStatus.escalated"),
    closed_true_positive: t("alertStatus.closed_true_positive"),
    closed_false_positive: t("alertStatus.closed_false_positive"),
    suppressed: t("alertStatus.suppressed"),
  }
  const [alerts, setAlerts] = useState<Alert[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [statusFilter, setStatusFilter] = useState(() => searchParams.get("status") ?? "")
  const [severityFilter, setSeverityFilter] = useState<AlertSeverity | "">(() => (searchParams.get("severity") as AlertSeverity | "") || "")
  const [scenarioFilter, setScenarioFilter] = useState(() => searchParams.get("scenario_id") ?? "")
  const [assigneeFilter, setAssigneeFilter] = useState(() => searchParams.get("assignee") ?? "")
  const [mine, setMine] = useState(() => searchParams.get("mine") === "true")
  const [teamFilter, setTeamFilter] = useState(() => searchParams.get("team") ?? "")
  const [unassigned, setUnassigned] = useState(() => searchParams.get("unassigned") === "true")
  const [overdue, setOverdue] = useState(() => searchParams.get("overdue") === "true")
	const [search, setSearch] = useState(() => searchParams.get("search") ?? "")
	const [minAgeDays, setMinAgeDays] = useState(() => searchParams.get("min_age_days") ?? "")
	const [maxAgeDays, setMaxAgeDays] = useState(() => searchParams.get("max_age_days") ?? "")
	const [pageCursor, setPageCursor] = useState(() => searchParams.get("cursor") ?? "")
	const [cursorHistory, setCursorHistory] = useState<string[]>([])
	const [nextCursor, setNextCursor] = useState<string | null>(null)
	const filterKey = `${statusFilter}|${severityFilter}|${scenarioFilter}|${assigneeFilter}|${mine}|${teamFilter}|${unassigned}|${overdue}|${search}|${minAgeDays}|${maxAgeDays}`
	const previousFilterKey = useRef(filterKey)
	const requestGeneration = useRef(0)
  const { data: directory } = useApi(api.operators.directory)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60000)
    return () => window.clearInterval(timer)
  }, [])
  useEffect(() => {
    const next = new URLSearchParams()
    if (statusFilter) next.set("status", statusFilter)
    if (severityFilter) next.set("severity", severityFilter)
    if (scenarioFilter) next.set("scenario_id", scenarioFilter)
    if (assigneeFilter) next.set("assignee", assigneeFilter)
    if (mine) next.set("mine", "true")
    if (teamFilter) next.set("team", teamFilter)
    if (unassigned) next.set("unassigned", "true")
    if (overdue) next.set("overdue", "true")
		if (search) next.set("search", search)
		if (minAgeDays) next.set("min_age_days", minAgeDays)
		if (maxAgeDays) next.set("max_age_days", maxAgeDays)
		if (pageCursor) next.set("cursor", pageCursor)
		setSearchParams(next, { replace: true })
	}, [statusFilter, severityFilter, scenarioFilter, assigneeFilter, mine, teamFilter, unassigned, overdue, search, minAgeDays, maxAgeDays, pageCursor, setSearchParams])
	useEffect(() => {
		if (previousFilterKey.current === filterKey) return
		previousFilterKey.current = filterKey
		setPageCursor("")
		setCursorHistory([])
	}, [filterKey])

  // 一括ケース統合（the case-management workflow §アラートの一括処理）: 選択済みアラート
  // の ID をそのまま bulk-case へ渡す（既存ケースに追加、または新規ケースと
  // してまとめる）。
  const [selected, setSelected] = useState<Set<string>>(new Set())

  // 一括クローズ（the case-management workflow §アラートの一括処理）: bulk-close は
  // シナリオID・期間・severity の「フィルタ条件」でアラートを絞り込んで
  // CLOSED にする API であり、個別のアラートID指定ではない。そのため UI も
  // チェックボックス選択とは独立したフィルタ入力フォームとする。
  const [closeScenarioId, setCloseScenarioId] = useState("")
  const [closeSeverity, setCloseSeverity] = useState<AlertSeverity | "">("")
  const [closeReason, setCloseReason] = useState("")

  async function reload() {
		const generation = ++requestGeneration.current
		setLoading(true)
		try {
			const age = Number.parseInt(minAgeDays, 10)
			const maxAge = Number.parseInt(maxAgeDays, 10)
			const page = await api.alerts.list({ sort: "risk", cursor: pageCursor || undefined, limit: 200, status: statusFilter || undefined, severity: severityFilter || undefined, scenarioId: scenarioFilter || undefined, assignee: assigneeFilter || undefined, mine: mine || undefined, team: teamFilter || undefined, unassigned: unassigned || undefined, overdue: overdue || undefined, search: search || undefined, minAgeDays: Number.isFinite(age) && age > 0 ? age : undefined, maxAgeDays: Number.isFinite(maxAge) && maxAge > 0 ? maxAge : undefined })
			if (generation !== requestGeneration.current) return
			setAlerts([...page.data].sort((left, right) => compareRiskValues({ risk: left.severity, created_at: left.updated_at, id: left.id }, { risk: right.severity, created_at: right.updated_at, id: right.id })))
			setNextCursor(page.pagination.next_cursor ?? null)
			setError(null)
		} catch (err) {
			if (generation !== requestGeneration.current) return
			setError(translateApiError(err, t))
		} finally {
			if (generation === requestGeneration.current) setLoading(false)
    }
  }

  useEffect(() => {
    // Start the load outside the effect's synchronous phase: setLoading(true) at
    // the head of reload() would otherwise be a synchronous setState in an
    // effect, which cascades an extra render.
    void Promise.resolve().then(reload)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload is intentionally scoped to queue filters
	}, [statusFilter, severityFilter, scenarioFilter, assigneeFilter, mine, teamFilter, unassigned, overdue, search, minAgeDays, maxAgeDays, pageCursor])

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
      setActionError(translateApiError(err, t))
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
      setActionError(translateApiError(err, t))
    } finally {
      setBusy(false)
    }
  }

  if (loading) {
    return <TableSkeleton />
  }

	if (error) {
		return <div role="alert" className="space-y-3 p-12 text-center text-destructive"><p>{t("alerts.error")}</p><Button type="button" variant="outline" onClick={() => void reload()}>{t("alertDetail.retry")}</Button></div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("alerts.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("alerts.count", { count: alerts?.length ?? 0 })}</p>
      </div>

      <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-muted/40 p-3" aria-label={t("alerts.queue.title")}>
        <span className="text-sm font-medium">{t("alerts.queue.title")}</span>
        <select aria-label={t("alerts.queue.status")} value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} className="h-9 rounded-md border bg-background px-2 text-sm">
          <option value="">{t("alerts.queue.allStatuses")}</option>
          <option value="open">{t("alertStatus.open")}</option>
          <option value="investigating">{t("alertStatus.investigating")}</option>
          <option value="escalated">{t("alertStatus.escalated")}</option>
          <option value="closed_true_positive">{t("alertStatus.closed_true_positive")}</option>
          <option value="closed_false_positive">{t("alertStatus.closed_false_positive")}</option>
        </select>
        <select aria-label={t("alerts.queue.severity")} value={severityFilter} onChange={(e) => setSeverityFilter(e.target.value as AlertSeverity | "")} className="h-9 rounded-md border bg-background px-2 text-sm">
          <option value="">{t("alerts.queue.severity")}</option>
          <option value="low">{t("alertSeverity.low")}</option>
          <option value="medium">{t("alertSeverity.medium")}</option>
          <option value="high">{t("alertSeverity.high")}</option>
          <option value="critical">{t("alertSeverity.critical")}</option>
        </select>
        <input aria-label={t("alerts.queue.scenario")} value={scenarioFilter} onChange={(e) => setScenarioFilter(e.target.value)} placeholder={t("alerts.queue.scenario")} className="h-9 w-32 rounded-md border bg-background px-2 text-sm" />
        <input list="alert-directory-users" aria-label={t("alerts.queue.assignee")} value={assigneeFilter} onChange={(e) => { setAssigneeFilter(e.target.value); setMine(false) }} placeholder={t("alerts.queue.assignee")} className="h-9 w-32 rounded-md border bg-background px-2 text-sm" />
        <input list="alert-directory-teams" aria-label={t("alerts.queue.team")} value={teamFilter} onChange={(e) => setTeamFilter(e.target.value)} placeholder={t("alerts.queue.team")} className="h-9 w-32 rounded-md border bg-background px-2 text-sm" />
        <input aria-label={t("alerts.queue.search")} value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t("alerts.queue.search")} className="h-9 w-40 rounded-md border bg-background px-2 text-sm" />
		<input aria-label={t("alerts.queue.age")} type="number" min={1} value={minAgeDays} onChange={(e) => setMinAgeDays(e.target.value)} placeholder={t("alerts.queue.age")} className="h-9 w-24 rounded-md border bg-background px-2 text-sm" />
		<input aria-label={t("alerts.queue.maxAge")} type="number" min={1} value={maxAgeDays} onChange={(e) => setMaxAgeDays(e.target.value)} placeholder={t("alerts.queue.maxAge")} className="h-9 w-24 rounded-md border bg-background px-2 text-sm" />
        <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={mine} onChange={(e) => { setMine(e.target.checked); if (e.target.checked) setAssigneeFilter("") }} />{t("alerts.queue.myWork")}</label>
        <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={unassigned} onChange={(e) => setUnassigned(e.target.checked)} />{t("alerts.queue.unassigned")}</label>
        <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={overdue} onChange={(e) => setOverdue(e.target.checked)} />{t("alerts.queue.overdue")}</label>
      </div>
      <datalist id="alert-directory-users">{(directory?.users ?? []).map((user) => <option key={user.id} value={user.id}>{user.email}</option>)}</datalist>
      <datalist id="alert-directory-teams">{(directory?.teams ?? []).map((team) => <option key={team} value={team} />)}</datalist>

      <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-muted/40 p-4">
        <span className="text-sm font-medium">{t("alerts.bulkClose.label")}</span>
		<input
		  type="text"
		  aria-label={t("alerts.bulkClose.scenarioIdPlaceholder")}
          placeholder={t("alerts.bulkClose.scenarioIdPlaceholder")}
          value={closeScenarioId}
          onChange={(e) => setCloseScenarioId(e.target.value)}
          className="h-9 w-40 rounded-md border border-input bg-background px-3 text-sm"
        />
		<select
		  aria-label={t("alerts.bulkClose.severityPlaceholder")}
          value={closeSeverity}
          onChange={(e) => setCloseSeverity(e.target.value as AlertSeverity | "")}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="">{t("alerts.bulkClose.severityPlaceholder")}</option>
          <option value="low">{t("alertSeverity.low")}</option>
          <option value="medium">{t("alertSeverity.medium")}</option>
          <option value="high">{t("alertSeverity.high")}</option>
          <option value="critical">{t("alertSeverity.critical")}</option>
        </select>
		<input
		  type="text"
		  aria-label={t("alerts.bulkClose.reasonPlaceholder")}
          placeholder={t("alerts.bulkClose.reasonPlaceholder")}
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
          {t("alerts.bulkClose.submit")}
        </Button>
      </div>

      {selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-muted/40 p-4">
          <span className="text-sm font-medium">{t("alerts.bulkCase.selectedCount", { count: selected.size })}</span>
          <Button size="sm" variant="outline" disabled={busy} onClick={handleBulkCase}>
            {t("alerts.bulkCase.submit")}
          </Button>
        </div>
      )}

	  {actionError && <p role="alert" className="text-sm text-destructive">{actionError}</p>}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10" />
              <TableHead>{t("alerts.table.header.severity")}</TableHead>
              <TableHead>{t("alerts.table.header.status")}</TableHead>
              <TableHead>{t("alerts.table.header.customerId")}</TableHead>
              <TableHead>{t("alerts.table.header.scenario")}</TableHead>
              <TableHead>{t("alerts.table.header.score")}</TableHead>
              <TableHead>{t("alerts.table.header.description")}</TableHead>
              <TableHead>{t("alerts.table.header.detectedAt")}</TableHead>
              <TableHead>{t("alerts.queue.age")}</TableHead>
              <TableHead>{t("alerts.table.header.assignedTo")}</TableHead>
              <TableHead>{t("alerts.table.header.dueAt")}</TableHead>
              <TableHead>{t("alerts.queue.sla")}</TableHead>
              <TableHead>{t("alerts.table.header.updatedAt")}</TableHead>
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
                        {severityLabels[a.severity] ?? a.severity}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {statusLabels[a.status] ?? a.status}
                    </Badge>
                    {/* A suppressed alert is withheld, not resolved. Showing
                        the status without the reason leaves an operator with
                        no way to tell why it is not in their queue. */}
                    {a.suppressed && (
                      <Badge variant="secondary" className="ml-1" title={a.suppression_reason}>
                        {t("alertStatus.suppressed")}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-sm">{a.customer_id}</TableCell>
                  <TableCell className="font-mono text-sm">{a.scenario_id}</TableCell>
                  <TableCell>{a.score.toFixed(1)}</TableCell>
                  <TableCell className="max-w-[300px] truncate">{a.description}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(a.detected_at, i18n.language)}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatAge(a.detected_at, i18n.language, t, now)}</TableCell>
                  <TableCell>{a.assigned_to || a.assigned_team || "-"}</TableCell>
                  <TableCell className="whitespace-nowrap">{a.due_at ? formatDateTime(a.due_at, i18n.language) : "-"}</TableCell>
                  <TableCell><Badge variant={a.due_at && new Date(a.due_at).getTime() < now ? "destructive" : "outline"}>{slaState(a.due_at, t, now)}</Badge></TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(a.updated_at, i18n.language)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={13} className="h-24 text-center text-muted-foreground">
                  {t("alerts.table.empty")}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {alerts && (pageCursor || nextCursor) && <div className="flex items-center justify-center gap-3 text-sm">
        <Button type="button" variant="outline" size="sm" disabled={cursorHistory.length === 0 || loading} onClick={() => { const previous = cursorHistory[cursorHistory.length - 1] ?? ""; setPageCursor(previous); setCursorHistory(cursorHistory.slice(0, -1)) }}>{t("list.previous")}</Button>
        <span className="text-muted-foreground">{t("alerts.queue.page")}</span>
        <Button type="button" variant="outline" size="sm" disabled={!nextCursor || loading} onClick={() => { if (!nextCursor) return; setCursorHistory([...cursorHistory, pageCursor]); setPageCursor(nextCursor) }}>{t("list.next")}</Button>
      </div>}
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
