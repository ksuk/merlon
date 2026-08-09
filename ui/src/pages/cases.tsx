import { Badge } from "@/components/ui/badge"
import { formatDateTime } from "@/lib/format"
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
import { api, type CasePriority } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { compareRiskValues } from "@/lib/risk"
import { Plus } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useSearchParams } from "react-router"

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}


function formatAge(iso: string, label: (key: string, options?: Record<string, unknown>) => string, now: number) {
  const days = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 86400000))
  return label("cases.queue.ageValue", { days })
}

function slaState(dueAt: string | undefined, label: (key: string) => string, now: number) {
  if (!dueAt) return label("cases.queue.slaUnassigned")
  return new Date(dueAt).getTime() < now ? label("cases.queue.slaOverdue") : label("cases.queue.slaOnTrack")
}

export function CasesPage() {
  const { t, i18n } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  const [now, setNow] = useState(() => Date.now())
  const priorityLabels: Record<CasePriority, string> = {
    low: t("casePriority.low"),
    medium: t("casePriority.medium"),
    high: t("casePriority.high"),
    critical: t("casePriority.critical"),
  }
  const statusLabels: Record<string, string> = {
    open: t("caseStatus.open"),
    new: t("caseStatus.new"),
    investigating: t("caseStatus.investigating"),
    escalated: t("caseStatus.escalated"),
    closed: t("caseStatus.closed"),
    reopened: t("caseStatus.reopened"),
    str_filed: t("caseStatus.str_filed"),
  }
  const [statusFilter, setStatusFilter] = useState(() => searchParams.get("status") ?? "")
  const [priorityFilter, setPriorityFilter] = useState<CasePriority | "">(() => (searchParams.get("priority") as CasePriority | "") || "")
  const [assigneeFilter, setAssigneeFilter] = useState(() => searchParams.get("assignee") ?? "")
  const [mine, setMine] = useState(() => searchParams.get("mine") === "true")
  const [teamFilter, setTeamFilter] = useState(() => searchParams.get("team") ?? "")
  const [unassigned, setUnassigned] = useState(() => searchParams.get("unassigned") === "true")
  const [overdue, setOverdue] = useState(() => searchParams.get("overdue") === "true")
  const [search, setSearch] = useState(() => searchParams.get("search") ?? "")
  const [dispositionFilter, setDispositionFilter] = useState(() => searchParams.get("disposition") ?? "")
	const [strCandidateFilter, setStrCandidateFilter] = useState(() => searchParams.get("str_candidate") ?? "")
	const [minAgeDays, setMinAgeDays] = useState(() => searchParams.get("min_age_days") ?? "")
  const [maxAgeDays, setMaxAgeDays] = useState(() => searchParams.get("max_age_days") ?? "")
  const [pageCursor, setPageCursor] = useState(() => searchParams.get("cursor") ?? "")
  const [cursorHistory, setCursorHistory] = useState<string[]>([])
  const [refreshKey, setRefreshKey] = useState(0)
  const previousFilterKey = useRef("")
  const { data: directory } = useApi(api.operators.directory)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60000)
    return () => window.clearInterval(timer)
  }, [])
  useEffect(() => {
    const next = new URLSearchParams()
    if (statusFilter) next.set("status", statusFilter)
    if (priorityFilter) next.set("priority", priorityFilter)
    if (assigneeFilter) next.set("assignee", assigneeFilter)
    if (mine) next.set("mine", "true")
    if (teamFilter) next.set("team", teamFilter)
    if (unassigned) next.set("unassigned", "true")
    if (overdue) next.set("overdue", "true")
    if (search) next.set("search", search)
    if (dispositionFilter) next.set("disposition", dispositionFilter)
		if (strCandidateFilter) next.set("str_candidate", strCandidateFilter)
		if (minAgeDays) next.set("min_age_days", minAgeDays)
		if (maxAgeDays) next.set("max_age_days", maxAgeDays)
		if (pageCursor) next.set("cursor", pageCursor)
		setSearchParams(next, { replace: true })
	}, [statusFilter, priorityFilter, assigneeFilter, mine, teamFilter, unassigned, overdue, search, dispositionFilter, strCandidateFilter, minAgeDays, maxAgeDays, pageCursor, setSearchParams])
	const filterKey = `${statusFilter}|${priorityFilter}|${assigneeFilter}|${mine}|${teamFilter}|${unassigned}|${overdue}|${search}|${dispositionFilter}|${strCandidateFilter}|${minAgeDays}|${maxAgeDays}`
	useEffect(() => {
		if (!previousFilterKey.current) {
			previousFilterKey.current = filterKey
			return
		}
		if (previousFilterKey.current === filterKey) return
		previousFilterKey.current = filterKey
		setPageCursor("")
		setCursorHistory([])
	}, [filterKey])
  const { data: page, loading, error } = useApi(
    () => {
		const age = Number.parseInt(minAgeDays, 10)
		const maxAge = Number.parseInt(maxAgeDays, 10)
		return api.cases.list({ sort: "risk", cursor: pageCursor || undefined, limit: 200, status: statusFilter || undefined, priority: priorityFilter || undefined, assignee: assigneeFilter || undefined, mine: mine || undefined, team: teamFilter || undefined, unassigned: unassigned || undefined, disposition: dispositionFilter || undefined, strCandidate: strCandidateFilter === "" ? undefined : strCandidateFilter === "true", overdue: overdue || undefined, search: search || undefined, minAgeDays: Number.isFinite(age) && age > 0 ? age : undefined, maxAgeDays: Number.isFinite(maxAge) && maxAge > 0 ? maxAge : undefined })
    },
    `${filterKey}:${pageCursor}:${refreshKey}`,
  )
  const cases = page?.data
    ? [...page.data].sort((left, right) => compareRiskValues({ risk: left.priority, created_at: left.updated_at, id: left.id }, { risk: right.priority, created_at: right.updated_at, id: right.id }))
    : undefined
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [creationError, setCreationError] = useState<string | null>(null)
  const [priority, setPriority] = useState<CasePriority>("medium")
  const custRef = useRef<HTMLInputElement>(null)
  const alertRef = useRef<HTMLInputElement>(null)
  const assignRef = useRef<HTMLInputElement>(null)
  const summaryRef = useRef<HTMLInputElement>(null)

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const customerId = custRef.current?.value.trim()
    const summary = summaryRef.current?.value.trim()
    if (!customerId || !summary) return
    setCreating(true)
    setCreationError(null)
    const alertIds = alertRef.current?.value.trim()
      ? alertRef.current.value.split(",").map((s) => s.trim()).filter(Boolean)
      : []
    try {
      await api.cases.create({
        customer_id: customerId,
        alert_ids: alertIds,
        priority,
        assigned_to: assignRef.current?.value.trim() || undefined,
        summary,
      })
      window.location.reload()
    } catch (err) {
      setCreationError(translateApiError(err, t))
    } finally {
      setCreating(false)
    }
  }

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <div role="alert" className="space-y-3 p-12 text-center text-destructive"><p>{t("cases.error")}</p><Button type="button" variant="outline" onClick={() => setRefreshKey((key) => key + 1)}>{t("caseDetail.retry")}</Button></div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("cases.title")}</h1>
        <div className="flex items-center gap-2">
          <p className="text-sm text-muted-foreground">{t("cases.count", { count: cases?.length ?? 0 })}</p>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            {t("cases.createButton")}
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-muted/40 p-3" aria-label={t("cases.queue.title")}>
        <span className="text-sm font-medium">{t("cases.queue.title")}</span>
        <select aria-label={t("cases.queue.status")} value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} className="h-9 rounded-md border bg-background px-2 text-sm">
          <option value="">{t("cases.queue.allStatuses")}</option>
          <option value="open">{t("caseStatus.open")}</option>
          <option value="new">{t("caseStatus.new")}</option>
          <option value="investigating">{t("caseStatus.investigating")}</option>
          <option value="escalated">{t("caseStatus.escalated")}</option>
          <option value="reopened">{t("caseStatus.reopened")}</option>
          <option value="closed">{t("caseStatus.closed")}</option>
          <option value="str_filed">{t("caseStatus.str_filed")}</option>
        </select>
        <select aria-label={t("cases.queue.priority")} value={priorityFilter} onChange={(e) => setPriorityFilter(e.target.value as CasePriority | "")} className="h-9 rounded-md border bg-background px-2 text-sm">
          <option value="">{t("cases.queue.allPriorities")}</option>
          <option value="low">{priorityLabels.low}</option>
          <option value="medium">{priorityLabels.medium}</option>
          <option value="high">{priorityLabels.high}</option>
          <option value="critical">{priorityLabels.critical}</option>
        </select>
        <input list="case-directory-users" aria-label={t("cases.queue.assignee")} value={assigneeFilter} onChange={(e) => { setAssigneeFilter(e.target.value); setMine(false) }} placeholder={t("cases.queue.assignee")} className="h-9 w-32 rounded-md border bg-background px-2 text-sm" />
        <input list="case-directory-teams" aria-label={t("cases.queue.team")} value={teamFilter} onChange={(e) => setTeamFilter(e.target.value)} placeholder={t("cases.queue.team")} className="h-9 w-32 rounded-md border bg-background px-2 text-sm" />
        <input aria-label={t("cases.queue.disposition")} value={dispositionFilter} onChange={(e) => setDispositionFilter(e.target.value)} placeholder={t("cases.queue.disposition")} className="h-9 w-32 rounded-md border bg-background px-2 text-sm" />
        <select aria-label={t("cases.queue.strCandidate")} value={strCandidateFilter} onChange={(e) => setStrCandidateFilter(e.target.value)} className="h-9 rounded-md border bg-background px-2 text-sm">
          <option value="">{t("cases.queue.allCandidates")}</option>
          <option value="true">{t("cases.queue.candidatesOnly")}</option>
          <option value="false">{t("cases.queue.nonCandidates")}</option>
        </select>
        <input aria-label={t("cases.queue.search")} value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t("cases.queue.search")} className="h-9 w-40 rounded-md border bg-background px-2 text-sm" />
		<input aria-label={t("cases.queue.age")} type="number" min={1} value={minAgeDays} onChange={(e) => setMinAgeDays(e.target.value)} placeholder={t("cases.queue.age")} className="h-9 w-24 rounded-md border bg-background px-2 text-sm" />
		<input aria-label={t("cases.queue.maxAge")} type="number" min={1} value={maxAgeDays} onChange={(e) => setMaxAgeDays(e.target.value)} placeholder={t("cases.queue.maxAge")} className="h-9 w-24 rounded-md border bg-background px-2 text-sm" />
        <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={mine} onChange={(e) => { setMine(e.target.checked); if (e.target.checked) setAssigneeFilter("") }} />{t("cases.queue.myWork")}</label>
        <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={unassigned} onChange={(e) => setUnassigned(e.target.checked)} />{t("cases.queue.unassigned")}</label>
        <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={overdue} onChange={(e) => setOverdue(e.target.checked)} />{t("cases.queue.overdue")}</label>
      </div>
      <datalist id="case-directory-users">{(directory?.users ?? []).map((user) => <option key={user.id} value={user.id}>{user.email}</option>)}</datalist>
      <datalist id="case-directory-teams">{(directory?.teams ?? []).map((team) => <option key={team} value={team} />)}</datalist>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("cases.form.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            {creationError && <p role="alert" className="mb-3 text-sm text-destructive">{creationError}</p>}
            <form onSubmit={handleCreate} className="space-y-3">
              <div className="flex flex-wrap items-end gap-3">
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.customerId")}</label>
                  <input ref={custRef} required placeholder="cust-001"
                    className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.alertIds")}</label>
                  <input ref={alertRef} placeholder="alert-001,alert-002"
                    className="w-48 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.assignedTo")}</label>
                  <input ref={assignRef} list="case-directory-users" placeholder="tanaka"
                    className="w-24 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium">{t("cases.form.priority")}</label>
                  <div className="flex gap-1">
                    {(["low", "medium", "high", "critical"] as const).map((p) => (
                      <button key={p} type="button" onClick={() => setPriority(p)}
                        className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors ${priority === p ? "border-primary bg-primary/10 text-primary" : "border-input text-muted-foreground hover:bg-accent"}`}>
                        {priorityLabels[p]}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("cases.form.summary")}</label>
                <input ref={summaryRef} required placeholder={t("cases.form.summaryPlaceholder")}
                  className="w-full rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>{t("cases.form.submit")}</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("cases.table.header.priority")}</TableHead>
              <TableHead>{t("cases.table.header.status")}</TableHead>
              <TableHead>{t("cases.table.header.customerId")}</TableHead>
              <TableHead>{t("cases.table.header.assignedTo")}</TableHead>
              <TableHead>{t("cases.table.header.summary")}</TableHead>
              <TableHead>{t("cases.table.header.createdAt")}</TableHead>
              <TableHead>{t("cases.queue.age")}</TableHead>
              <TableHead>{t("cases.queue.dueAt")}</TableHead>
              <TableHead>{t("cases.queue.sla")}</TableHead>
              <TableHead>{t("cases.queue.updatedAt")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cases && cases.length > 0 ? (
              cases.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>
                    <Link to={`/cases/${c.id}`}>
                      <Badge variant={PRIORITY_VARIANT[c.priority]}>
                        {priorityLabels[c.priority] ?? c.priority}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{statusLabels[c.status] ?? c.status}</Badge>
                    {c.str_candidate && c.status !== "str_filed" && <Badge className="ml-1" variant="secondary">{t("caseStatus.str_candidate")}</Badge>}
                  </TableCell>
                  <TableCell className="font-mono text-sm">
                    <Link to={`/customers/${c.customer_id}`} className="text-primary hover:underline">
                      {c.customer_id}
                    </Link>
                  </TableCell>
                  <TableCell>{c.assigned_to || c.assigned_team || "-"}</TableCell>
                  <TableCell className="max-w-[300px] truncate">{c.summary}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(c.created_at, i18n.language)}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatAge(c.created_at, t, now)}</TableCell>
                  <TableCell className="whitespace-nowrap">{c.due_at ? formatDateTime(c.due_at, i18n.language) : "-"}</TableCell>
                  <TableCell><Badge variant={c.due_at && new Date(c.due_at).getTime() < now ? "destructive" : "outline"}>{slaState(c.due_at, t, now)}</Badge></TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(c.updated_at, i18n.language)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={10} className="h-24 text-center text-muted-foreground">
                  {t("cases.table.empty")}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {page && (pageCursor || page.pagination.next_cursor) && <div className="flex items-center justify-center gap-3 text-sm">
		<Button type="button" variant="outline" size="sm" disabled={cursorHistory.length === 0 || loading} onClick={() => { const previous = cursorHistory[cursorHistory.length - 1] ?? ""; setPageCursor(previous); setCursorHistory(cursorHistory.slice(0, -1)) }}>{t("list.previous")}</Button>
		<span className="text-muted-foreground">{t("cases.queue.page")}</span>
		<Button type="button" variant="outline" size="sm" disabled={!page.pagination.next_cursor || loading} onClick={() => { if (!page.pagination.next_cursor) return; setCursorHistory([...cursorHistory, pageCursor]); setPageCursor(page.pagination.next_cursor!) }}>{t("list.next")}</Button>
	  </div>}
	  {page && !page.pagination.has_more && <p className="text-center text-xs text-muted-foreground">{t("list.allLoaded")}</p>}
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
