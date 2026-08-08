import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { ApiError, api, type CasePriority, type CaseStatus, type Customer, type RelatedCase } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { ArrowLeft, Send } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function CaseDetailPage() {
  const { t, i18n } = useTranslation()
  const priorityLabels: Record<string, string> = {
    low: t("casePriority.low"),
    medium: t("casePriority.medium"),
    high: t("casePriority.high"),
    critical: t("casePriority.critical"),
  }
  const statusLabels: Record<CaseStatus, string> = {
    open: t("caseStatus.open"),
    new: t("caseStatus.new"),
    investigating: t("caseStatus.investigating"),
    escalated: t("caseStatus.escalated"),
    closed: t("caseStatus.closed"),
    reopened: t("caseStatus.reopened"),
    str_filed: t("caseStatus.str_filed"),
  }
  // the case-management workflow §ケースのステータス遷移の遷移図どおり
  // （NEW→INVESTIGATING→{ESCALATED→INVESTIGATING(差し戻し), CLOSED, STR_FILED}）。
  // CLOSED→REOPENED は理由必須・Analyst以上のため、別途の再オープンフォームで扱う
  // （このテーブルには含めない）。
  const statusTransitions: Record<CaseStatus, { label: string; value: CaseStatus }[]> = {
    open: [{ label: t("caseDetail.transitions.startInvestigation"), value: "investigating" }],
    new: [{ label: t("caseDetail.transitions.startInvestigation"), value: "investigating" }],
    investigating: [
      { label: t("caseDetail.transitions.escalate"), value: "escalated" },
      { label: t("caseDetail.transitions.close"), value: "closed" },
      { label: t("caseDetail.transitions.fileStr"), value: "str_filed" },
    ],
    escalated: [{ label: t("caseDetail.transitions.rollback"), value: "investigating" }],
    closed: [],
    reopened: [{ label: t("caseDetail.transitions.reinvestigate"), value: "investigating" }],
    str_filed: [],
  }
  const eddStageLabels: { key: keyof Customer; label: string; variant: "medium" | "high" | "critical" }[] = [
    { key: "edd_stage3_notified_at", label: t("caseDetail.edd.stage3"), variant: "critical" },
    { key: "edd_stage2_notified_at", label: t("caseDetail.edd.stage2"), variant: "high" },
    { key: "edd_stage1_last_sent_at", label: t("caseDetail.edd.stage1"), variant: "medium" },
  ]
  // eddStageDisplay picks the highest-reached EDD escalation stage for
  // display (the case-management workflow §EDD未実施継続時の段階的措置). Returns null
  // when the customer has no open EDD requirement.
  function eddStageDisplay(customer: Customer | null) {
    if (!customer?.edd_requested_at) return null
    for (const stage of eddStageLabels) {
      if (customer[stage.key]) return stage
    }
    return { key: "edd_requested_at" as const, label: t("caseDetail.edd.requested"), variant: "medium" as const }
  }
  const { id } = useParams<{ id: string }>()
  const { data: caseData, loading, error } = useApi(
    useCallback(() => api.cases.get(id!), [id]),
  )
  const [updating, setUpdating] = useState(false)
  const [conflictError, setConflictError] = useState<string | null>(null)
  const [addingNote, setAddingNote] = useState(false)
  const noteRef = useRef<HTMLTextAreaElement>(null)

  // ケース間の関連付け（the case-management workflow §ケース間の関連付け）。
  const [relatedCases, setRelatedCases] = useState<RelatedCase[] | null>(null)

  // EDD段階表示（the case-management workflow §EDD未実施継続時の段階的措置）。
  const [customer, setCustomer] = useState<Customer | null>(null)

  // 再オープン（理由必須・Analyst以上、the case-management workflow「再オープン時は
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
    setConflictError(null)
    try {
      await api.cases.update(id, { status, expected_updated_at: caseData!.updated_at })
      window.location.reload()
    } catch (err) {
      setConflictError(
        err instanceof ApiError && err.status === 409
          ? t("caseDetail.conflict")
          : translateApiError(err, t),
      )
      setUpdating(false)
    }
  }

  async function handleReopen(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !reopenReason.trim()) return
    setReopening(true)
    setReopenError(null)
    setConflictError(null)
    try {
      await api.cases.update(id, {
        status: "reopened",
        reason: reopenReason.trim(),
        expected_updated_at: caseData!.updated_at,
      })
      window.location.reload()
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setConflictError(t("caseDetail.conflict"))
      } else {
        setReopenError(translateApiError(err, t))
      }
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
          <ArrowLeft className="h-4 w-4" /> {t("caseDetail.backToList")}
        </Link>
        <p className="text-destructive">{t("caseDetail.error")}</p>
      </div>
    )
  }

  const transitions = statusTransitions[caseData.status] ?? []
  const eddStage = eddStageDisplay(customer)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-4">
        <Link to="/cases" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("caseDetail.back")}
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{t("caseDetail.title")}</h1>
        <Badge variant={PRIORITY_VARIANT[caseData.priority]}>
          {priorityLabels[caseData.priority]}
        </Badge>
        <Badge variant="outline">{statusLabels[caseData.status]}</Badge>
        {eddStage && <Badge variant={eddStage.variant}>{eddStage.label}</Badge>}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.info.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">ID</dt>
                <dd className="font-mono">{caseData.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.customerId")}</dt>
                <dd>
                  <Link to={`/customers/${caseData.customer_id}`} className="font-mono text-primary underline-offset-4 hover:underline">
                    {caseData.customer_id}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.assignedTo")}</dt>
                <dd>{caseData.assigned_to || "-"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.createdAt")}</dt>
                <dd>{formatDateTime(caseData.created_at, i18n.language)}</dd>
              </div>
              {caseData.closed_at && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("caseDetail.info.closedAt")}</dt>
                  <dd>{formatDateTime(caseData.closed_at, i18n.language)}</dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.summary.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{caseData.summary}</p>
            {caseData.alert_ids.length > 0 && (
              <div className="mt-4">
                <p className="mb-2 text-xs font-medium text-muted-foreground">{t("caseDetail.summary.relatedAlerts")}</p>
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
            <CardTitle className="text-base">{t("caseDetail.transitions.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            {conflictError && (
              <div role="alert" className="mb-3 rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">
                <p>{conflictError}</p>
                <Button variant="outline" size="sm" className="mt-2" onClick={() => window.location.reload()}>
                  {t("caseDetail.reload")}
                </Button>
              </div>
            )}
            <div className="flex gap-2">
              {transitions.map((transition) => (
                <Button
                  key={transition.value}
                  variant={transition.value === "closed" ? "destructive" : "outline"}
                  size="sm"
                  disabled={updating}
                  onClick={() => handleStatusChange(transition.value)}
                >
                  {transition.label}
                </Button>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {caseData.status === "closed" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.reopen.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleReopen} className="flex gap-2">
              <input
                type="text"
                value={reopenReason}
                onChange={(e) => setReopenReason(e.target.value)}
                placeholder={t("caseDetail.reopen.placeholder")}
                className="flex-1 rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
              <Button type="submit" size="sm" disabled={reopening || !reopenReason.trim()}>
                {t("caseDetail.reopen.submit")}
              </Button>
            </form>
            {reopenError && <p className="mt-2 text-sm text-destructive">{reopenError}</p>}
          </CardContent>
        </Card>
      )}

      {caseData.reopen_reason && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.reopenReason.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{caseData.reopen_reason}</p>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("caseDetail.relatedCases.title")}</CardTitle>
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
                    <Badge variant="outline">{statusLabels[rc.case.status]}</Badge>
                    <Badge variant="secondary" className="text-xs">
                      {rc.link_type === "auto" ? t("caseDetail.relatedCases.auto") : t("caseDetail.relatedCases.manual")}
                    </Badge>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{t("caseDetail.relatedCases.empty")}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("caseDetail.notes.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {caseData.notes && caseData.notes.length > 0 ? (
            <div className="space-y-3">
              {caseData.notes.map((note) => (
                <div key={note.id} className="rounded-lg bg-muted/50 p-3">
                  <div className="mb-1 flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="font-medium">{note.author}</span>
                    <span>{formatDateTime(note.created_at, i18n.language)}</span>
                  </div>
                  <p className="text-sm">{note.content}</p>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{t("caseDetail.notes.empty")}</p>
          )}

          {caseData.status !== "closed" && (
            <form onSubmit={handleAddNote} className="flex gap-2">
              <textarea
                ref={noteRef}
                placeholder={t("caseDetail.notes.placeholder")}
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
