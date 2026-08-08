import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { ApiError, api, type AlertDecisionEvent, type AlertSeverity, type AlertStatus } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { ArrowLeft } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

const SEVERITY_VARIANT: Record<AlertSeverity, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function AlertDetailPage() {
  const { t, i18n } = useTranslation()
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
  const statusTransitions: Record<AlertStatus, { label: string; value: AlertStatus }[]> = {
    // suppressed is whitelist/system-only: an operator PATCH cannot leave it,
    // so the surface offers no transition out of it.
    suppressed: [],
    open: [
      { label: t("alertDetail.transitions.startInvestigation"), value: "investigating" },
      { label: t("alertDetail.transitions.escalate"), value: "escalated" },
    ],
    investigating: [
      { label: t("alertDetail.transitions.escalate"), value: "escalated" },
      { label: t("alertDetail.transitions.closeTruePositive"), value: "closed_true_positive" },
      { label: t("alertDetail.transitions.closeFalsePositive"), value: "closed_false_positive" },
    ],
    escalated: [
      { label: t("alertDetail.transitions.closeTruePositive"), value: "closed_true_positive" },
      { label: t("alertDetail.transitions.closeFalsePositive"), value: "closed_false_positive" },
    ],
	    closed_true_positive: [{ label: t("alertDetail.transitions.reopen"), value: "investigating" }],
	    closed_false_positive: [{ label: t("alertDetail.transitions.reopen"), value: "investigating" }],
	  }
  const { id } = useParams<{ id: string }>()
  const { data: alert, loading, error } = useApi(
    useCallback(() => api.alerts.get(id!), [id]),
  )
  const { data: directory } = useApi(api.operators.directory)
  const [updating, setUpdating] = useState(false)
  const [conflictError, setConflictError] = useState<string | null>(null)
  const [pendingStatus, setPendingStatus] = useState<AlertStatus | null>(null)
  const [rationale, setRationale] = useState("")
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionHistoryError, setDecisionHistoryError] = useState<string | null>(null)
  const [decisions, setDecisions] = useState<AlertDecisionEvent[]>([])
  const [assignedTo, setAssignedTo] = useState("")
  const [assignedTeam, setAssignedTeam] = useState("")
  const [dueAt, setDueAt] = useState("")
  const [assignmentBusy, setAssignmentBusy] = useState(false)
  const decisionDialogRef = useRef<HTMLDivElement>(null)
  const decisionRationaleRef = useRef<HTMLTextAreaElement>(null)
  const decisionTriggerRef = useRef<HTMLButtonElement>(null)

  const loadDecisionHistory = useCallback(async () => {
    if (!id || !alert) return
    setDecisionHistoryError(null)
    try {
      const result = await api.alerts.decisions(id)
      setDecisions(Array.isArray(result) ? result : [])
    } catch (err) {
      setDecisionHistoryError(translateApiError(err, t))
    }
  }, [alert, id, t])

  useEffect(() => {
    // Load history after the primary alert request has completed. Besides
    // avoiding a race between the two requests, this keeps the detail page
    // usable for deployments that have not enabled the additive history
    // endpoint yet.
    void Promise.resolve().then(loadDecisionHistory)
  }, [loadDecisionHistory])

  useEffect(() => {
    if (!pendingStatus) return
    decisionRationaleRef.current?.focus()
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault()
        setPendingStatus(null)
        setRationale("")
        decisionTriggerRef.current?.focus()
        return
      }
      if (event.key !== "Tab" || !decisionDialogRef.current) return
      const focusable = Array.from(decisionDialogRef.current.querySelectorAll<HTMLElement>(
        "button, textarea, input, select, a[href], [tabindex]:not([tabindex='-1'])",
      )).filter((element) => !element.hasAttribute("disabled"))
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [pendingStatus])

  useEffect(() => {
    if (!alert) return
    void Promise.resolve().then(() => {
      setAssignedTo(alert.assigned_to ?? "")
      setAssignedTeam(alert.assigned_team ?? "")
      setDueAt(alert.due_at ? alert.due_at.slice(0, 16) : "")
    })
  }, [alert])

  async function handleSaveAssignment(e: React.FormEvent) {
    e.preventDefault()
    if (!id) return
    setAssignmentBusy(true)
    setConflictError(null)
    try {
	      await api.alerts.updateQueue(id, { assigned_to: assignedTo, assigned_team: assignedTeam, ...(dueAt ? { due_at: new Date(dueAt).toISOString() } : { clear_due_at: true }), expected_updated_at: alert!.updated_at })
      window.location.reload()
    } catch (err) {
      setConflictError(err instanceof ApiError && err.status === 409 ? t("alertDetail.conflict") : translateApiError(err, t))
      setAssignmentBusy(false)
    }
  }

  async function handleStatusChange(status: AlertStatus) {
    if (!id) return
	    const requiresDecision = status.startsWith("closed_") || (alert != null && ["closed_true_positive", "closed_false_positive"].includes(alert.status) && status === "investigating")
	    if (requiresDecision && !pendingStatus) {
      setPendingStatus(status)
      setRationale("")
      setDecisionError(null)
      return
    }
    setUpdating(true)
    setConflictError(null)
    try {
	      await api.alerts.updateStatus(id, status, alert!.updated_at, requiresDecision ? { rationale: rationale.trim(), confirm: true } : undefined)
      window.location.reload()
    } catch (err) {
      setConflictError(
        err instanceof ApiError && err.status === 409
          ? t("alertDetail.conflict")
          : translateApiError(err, t),
      )
      setUpdating(false)
    }
  }

  async function confirmDecision(e: React.FormEvent) {
    e.preventDefault()
    if (!pendingStatus || !rationale.trim()) {
      setDecisionError(t("alertDetail.decision.rationaleRequired"))
      return
    }
    setDecisionError(null)
    try {
      await handleStatusChange(pendingStatus)
    } catch {
      // handleStatusChange keeps the dialog and reports the request error.
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
          <ArrowLeft className="h-4 w-4" /> {t("alertDetail.backToList")}
        </Link>
        <p className="text-destructive">{t("alertDetail.error")}</p>
      </div>
    )
  }

  const transitions = statusTransitions[alert.status] ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/alerts" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("alertDetail.back")}
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{t("alertDetail.title")}</h1>
        <Badge variant={SEVERITY_VARIANT[alert.severity]}>
          {severityLabels[alert.severity]}
        </Badge>
        <Badge variant="outline">{statusLabels[alert.status]}</Badge>
        {alert.suppressed && (
          <Badge variant="secondary" data-testid="alert-suppressed">
            {alert.suppression_reason
              ? t("alerts.suppressedReason", { reason: alert.suppression_reason })
              : t("alertStatus.suppressed")}
          </Badge>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("alertDetail.info.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">ID</dt>
                <dd className="font-mono">{alert.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.customerId")}</dt>
                <dd>
                  <Link to={`/customers/${alert.customer_id}`} className="font-mono text-primary underline-offset-4 hover:underline">
                    {alert.customer_id}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.scenario")}</dt>
                <dd>
                  <Link to="/rules" className="font-mono text-primary underline-offset-4 hover:underline">{alert.scenario_id}</Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.score")}</dt>
                <dd className="text-lg font-bold">{alert.score.toFixed(1)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("alertDetail.info.detectedAt")}</dt>
                <dd>{formatDateTime(alert.detected_at, i18n.language)}</dd>
              </div>
              {alert.resolved_at && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("alertDetail.info.resolvedAt")}</dt>
                  <dd>{formatDateTime(alert.resolved_at, i18n.language)}</dd>
                </div>
              )}
              {alert.resolved_by && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("alertDetail.info.resolvedBy")}</dt>
                  <dd>{alert.resolved_by}</dd>
                </div>
              )}
            </dl>
            <form onSubmit={handleSaveAssignment} className="mt-4 grid gap-2 border-t pt-4 sm:grid-cols-2">
              <input list="alert-detail-directory-users" value={assignedTo} onChange={(e) => setAssignedTo(e.target.value)} aria-label={t("alertDetail.info.assignedTo")} placeholder={t("alertDetail.info.assignedTo")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <input list="alert-detail-directory-teams" value={assignedTeam} onChange={(e) => setAssignedTeam(e.target.value)} aria-label={t("alertDetail.info.assignedTeam")} placeholder={t("alertDetail.info.assignedTeam")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <input type="datetime-local" value={dueAt} onChange={(e) => setDueAt(e.target.value)} aria-label={t("alertDetail.info.dueAt")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <Button type="submit" size="sm" disabled={assignmentBusy}>{t("alertDetail.info.saveAssignment")}</Button>
            </form>
            <datalist id="alert-detail-directory-users">{(directory?.users ?? []).map((user) => <option key={user.id} value={user.id}>{user.email}</option>)}</datalist>
            <datalist id="alert-detail-directory-teams">{(directory?.teams ?? []).map((team) => <option key={team} value={team} />)}</datalist>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("alertDetail.description.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{alert.description}</p>
            {alert.transaction_ids.length > 0 && (
              <div className="mt-4">
                <p className="mb-2 text-xs font-medium text-muted-foreground">{t("alertDetail.description.relatedTransactions")}</p>
                <div className="flex flex-wrap gap-1">
                  {alert.transaction_ids.map((tid) => (
                    <Link key={tid} to={`/transactions/${encodeURIComponent(tid)}`}>
                      <Badge variant="secondary" className="font-mono text-xs hover:bg-accent">{tid}</Badge>
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
            <CardTitle className="text-base">{t("alertDetail.transitions.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            {conflictError && (
              <div role="alert" className="mb-3 rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">
                <p>{conflictError}</p>
                <Button variant="outline" size="sm" className="mt-2" onClick={() => window.location.reload()}>
                  {t("alertDetail.reload")}
                </Button>
              </div>
            )}
            <div className="flex gap-2">
              {transitions.map((transition) => (
                <Button
                  key={transition.value}
                  variant={transition.value.startsWith("closed") ? "destructive" : "outline"}
                  size="sm"
                  disabled={updating}
                  onClick={(event) => {
                    decisionTriggerRef.current = event.currentTarget
                    void handleStatusChange(transition.value)
                  }}
                >
                  {transition.label}
                </Button>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {pendingStatus && (
        <Card ref={decisionDialogRef} role="dialog" aria-modal="true" aria-labelledby="alert-decision-title">
          <CardHeader>
            <CardTitle id="alert-decision-title" className="text-base">{t("alertDetail.decision.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="mb-3 text-sm text-muted-foreground">{t("alertDetail.decision.confirm", { outcome: statusLabels[pendingStatus] })}</p>
            <form onSubmit={confirmDecision} className="space-y-3">
              <label className="block text-sm font-medium">
                {t("alertDetail.decision.rationale")}
                <textarea ref={decisionRationaleRef} value={rationale} onChange={(e) => setRationale(e.target.value)} rows={3} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm" />
              </label>
              {decisionError && <p role="alert" className="text-sm text-destructive">{decisionError}</p>}
              <div className="flex gap-2">
	                <Button type="submit" variant={pendingStatus.startsWith("closed_") ? "destructive" : "default"} disabled={updating}>{t("alertDetail.decision.confirmButton")}</Button>
                <Button type="button" variant="ghost" onClick={() => { setPendingStatus(null); setRationale(""); decisionTriggerRef.current?.focus() }} disabled={updating}>{t("alertDetail.decision.cancel")}</Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("alertDetail.decision.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          {decisionHistoryError ? (
            <div role="alert" className="space-y-2 text-sm text-destructive">
              <p>{decisionHistoryError}</p>
              <Button type="button" variant="outline" size="sm" onClick={() => void loadDecisionHistory()}>{t("alertDetail.retry")}</Button>
            </div>
          ) : decisions.length > 0 ? (
            <ol className="space-y-2 text-sm">
              {decisions.map((decision) => <li key={decision.id} className="rounded-md bg-muted/50 p-3"><div className="flex flex-wrap gap-2 text-xs text-muted-foreground"><span>{formatDateTime(decision.created_at, i18n.language)}</span><span>{decision.actor}</span><span>{decision.from_status} → {decision.to_status}</span></div><p className="mt-1">{decision.rationale}</p></li>)}
            </ol>
          ) : <p className="text-sm text-muted-foreground">{t("alertDetail.decision.noHistory")}</p>}
        </CardContent>
      </Card>
    </div>
  )
}
