import { useState } from "react"
import { Link } from "react-router"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { useCan } from "@/hooks/use-session"
import { api, type CustomerReview, type CustomerReviewOutcome, type CustomerReviewStatus, type RiskTier } from "@/lib/api"

const statusVariant: Record<CustomerReviewStatus, "low" | "medium" | "high" | "critical" | "secondary"> = {
  scheduled: "secondary", due: "medium", overdue: "critical", in_progress: "high", blocked: "critical", completed: "low",
}
const tierVariant: Record<RiskTier, "low" | "medium" | "high"> = { low: "low", medium: "medium", high: "high" }

export function CustomerReviewsPage() {
  const { t, i18n } = useTranslation()
  const [status, setStatus] = useState<CustomerReviewStatus | "">("")
  const [refresh, setRefresh] = useState(0)
  const [selected, setSelected] = useState<CustomerReview | null>(null)
  const [assignedTo, setAssignedTo] = useState("")
  const [assignedTeam, setAssignedTeam] = useState("")
  const [outcome, setOutcome] = useState<CustomerReviewOutcome>("rating_unchanged")
  const [rationale, setRationale] = useState("")
  const [evidence, setEvidence] = useState("")
  const [mutationError, setMutationError] = useState<string | null>(null)
  const canWrite = useCan("cdd.score")
  const { data, loading, error } = useApi(() => api.customerReviews.list({ status: status || undefined, limit: 100 }), `${status}:${refresh}`)
  const reviews = data?.data ?? []
  const dueCount = reviews.filter((review) => review.status === "due").length
  const overdueCount = reviews.filter((review) => review.status === "overdue").length
  const coldStartCount = reviews.filter((review) => review.cycle === 1 && !review.previous_score_id).length

  async function mutate(action: () => Promise<unknown>) {
    setMutationError(null)
    try {
      await action()
      setSelected(null)
      setRefresh((value) => value + 1)
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : String(err))
    }
  }

  async function completeReview() {
    if (!selected || !rationale.trim() || !evidence.trim()) return
    let scope: Record<string, unknown>
    try {
      scope = { fields: JSON.parse(evidence) }
    } catch {
      scope = { fields: evidence.split(",").map((value) => value.trim()).filter(Boolean) }
    }
    await mutate(() => api.customerReviews.complete(selected.id, {
      outcome, rationale: rationale.trim(), evidence_refs: evidence.split(",").map((value) => value.trim()).filter(Boolean), scope, expected_version: selected.version,
    }))
    setRationale("")
    setEvidence("")
  }

  if (loading) return <div role="status" className="p-12 text-center text-muted-foreground">{t("customerReviews.loading")}</div>
  if (error) return <p role="alert" className="p-12 text-center text-destructive">{t("customerReviews.error", { error })}</p>

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div><h1 className="text-2xl font-bold tracking-tight">{t("customerReviews.title")}</h1><p className="text-sm text-muted-foreground">{t("customerReviews.subtitle")}</p></div>
        <label className="text-sm"><span className="sr-only">{t("customerReviews.filter")}</span><select value={status} onChange={(event) => setStatus(event.target.value as CustomerReviewStatus | "")} className="rounded-md border bg-background px-3 py-2"><option value="">{t("customerReviews.allStatuses")}</option>{(["scheduled", "due", "overdue", "in_progress", "blocked", "completed"] as CustomerReviewStatus[]).map((value) => <option key={value} value={value}>{t(`customerReviews.status.${value}`)}</option>)}</select></label>
      </div>
      <div className="grid gap-3 sm:grid-cols-3"><Stat label={t("customerReviews.stats.due")} value={dueCount} /><Stat label={t("customerReviews.stats.overdue")} value={overdueCount} /><Stat label={t("customerReviews.stats.coldStart")} value={coldStartCount} /></div>
      {mutationError && <p role="alert" className="rounded-md border border-destructive/50 p-3 text-sm text-destructive">{mutationError}</p>}
      <Card><CardHeader><CardTitle className="text-base">{t("customerReviews.queueTitle")}</CardTitle></CardHeader><CardContent><div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>{t("customerReviews.table.customer")}</TableHead><TableHead>{t("customerReviews.table.tier")}</TableHead><TableHead>{t("customerReviews.table.status")}</TableHead><TableHead>{t("customerReviews.table.due")}</TableHead><TableHead>{t("customerReviews.table.assignment")}</TableHead><TableHead>{t("customerReviews.table.actions")}</TableHead></TableRow></TableHeader><TableBody>{reviews.length === 0 ? <TableRow><TableCell colSpan={6} className="h-24 text-center text-muted-foreground">{t("customerReviews.empty")}</TableCell></TableRow> : reviews.map((review) => <ReviewRow key={review.id} review={review} canWrite={canWrite} t={t} locale={i18n.language} onSelect={setSelected} onStart={() => mutate(() => api.customerReviews.update(review.id, { action: review.status === "blocked" ? "resume" : "start", expected_version: review.version }))} onAssign={() => mutate(() => api.customerReviews.update(review.id, { assigned_to: assignedTo, assigned_team: assignedTeam, expected_version: review.version }))} />)}</TableBody></Table></div></CardContent></Card>
      {selected && <Card aria-label={t("customerReviews.completeTitle")}><CardHeader><CardTitle className="text-base">{t("customerReviews.completeTitle")}</CardTitle></CardHeader><CardContent className="space-y-3"><p className="text-sm">{t("customerReviews.selected", { id: selected.id })}</p><label className="block text-sm">{t("customerReviews.outcome")}<select value={outcome} onChange={(event) => setOutcome(event.target.value as CustomerReviewOutcome)} className="mt-1 w-full rounded-md border bg-background px-3 py-2">{(["rating_unchanged", "rating_changed", "escalated_to_edd", "unable_to_complete"] as CustomerReviewOutcome[]).map((value) => <option key={value} value={value}>{t(`customerReviews.outcomes.${value}`)}</option>)}</select></label><label className="block text-sm">{t("customerReviews.rationale")}<textarea value={rationale} onChange={(event) => setRationale(event.target.value)} className="mt-1 min-h-20 w-full rounded-md border bg-background px-3 py-2" /></label><label className="block text-sm">{t("customerReviews.evidence")}<input value={evidence} onChange={(event) => setEvidence(event.target.value)} placeholder={t("customerReviews.evidencePlaceholder")} className="mt-1 w-full rounded-md border bg-background px-3 py-2" /></label><div className="flex gap-2"><Button disabled={!canWrite || !rationale.trim() || !evidence.trim()} onClick={completeReview}>{t("customerReviews.complete")}</Button><Button variant="outline" onClick={() => setSelected(null)}>{t("customerReviews.cancel")}</Button></div></CardContent></Card>}
      <div className="grid gap-3 sm:grid-cols-2"><label className="text-sm">{t("customerReviews.assignTo")}<input value={assignedTo} onChange={(event) => setAssignedTo(event.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2" /></label><label className="text-sm">{t("customerReviews.assignTeam")}<input value={assignedTeam} onChange={(event) => setAssignedTeam(event.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2" /></label></div>
    </div>
  )
}

function ReviewRow({ review, canWrite, t, locale, onSelect, onStart, onAssign }: { review: CustomerReview; canWrite: boolean; t: TFunction; locale: string; onSelect: (review: CustomerReview) => void; onStart: () => void; onAssign: () => void }) {
  return <TableRow><TableCell><Link to={`/customers/${review.customer_id}`} className="font-mono text-primary underline-offset-4 hover:underline">{review.customer_id}</Link><div className="text-xs text-muted-foreground">{t("customerReviews.cycle", { cycle: review.cycle })}</div></TableCell><TableCell><Badge variant={tierVariant[review.tier]}>{t(`customerReviews.tier.${review.tier}`)}</Badge></TableCell><TableCell><Badge variant={statusVariant[review.status]}>{t(`customerReviews.status.${review.status}`)}</Badge></TableCell><TableCell>{new Date(review.due_at).toLocaleDateString(locale)}</TableCell><TableCell>{review.assigned_to || review.assigned_team || t("customerReviews.unassigned")}</TableCell><TableCell><div className="flex flex-wrap gap-1">{canWrite && review.status !== "completed" && <><Button size="sm" variant="outline" onClick={onStart}>{review.status === "in_progress" ? t("customerReviews.resume") : t("customerReviews.start")}</Button><Button size="sm" variant="outline" onClick={onAssign}>{t("customerReviews.assign")}</Button><Button size="sm" onClick={() => onSelect(review)}>{t("customerReviews.complete")}</Button></>}</div></TableCell></TableRow>
}

function Stat({ label, value }: { label: string; value: number }) { return <Card><CardContent className="p-4"><div className="text-xs text-muted-foreground">{label}</div><div className="text-2xl font-semibold">{value}</div></CardContent></Card> }
