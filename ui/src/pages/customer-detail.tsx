import { Badge } from "@/components/ui/badge"
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
import { CountrySelect, IdentityFields } from "@/components/identity-fields"
import { useApi } from "@/hooks/use-api"
import { usePolicy } from "@/hooks/use-policy"
import { api, type Customer, type EDDCompletionStatus, type RiskTier, type ScreenResult, type ScreeningResultRecord, type ScreeningResultStatus } from "@/lib/api"
import { identityRequirements } from "@/lib/identity"
import { ArrowLeft, Pencil, RefreshCw, Search } from "lucide-react"
import { useCallback, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

const TIER_VARIANT: Record<RiskTier, "low" | "medium" | "high"> = {
  low: "low",
  medium: "medium",
  high: "high",
}

// The five completion states an EDD window can be in. `overdue` and
// `completed` did not exist before: a finished window and one 200 days late
// both read as "open".
const EDD_STATUS_VARIANT: Record<EDDCompletionStatus, "low" | "medium" | "high" | "critical" | "secondary"> = {
  not_required: "secondary",
  open: "medium",
  overdue: "critical",
  escalated: "critical",
  completed: "low",
}

const EDD_STATUS_CLASS: Record<EDDCompletionStatus, string> = {
  not_required: "border-muted bg-muted/40 text-foreground",
  open: "border-amber-300 bg-amber-50 text-amber-950",
  overdue: "border-red-300 bg-red-50 text-red-950",
  escalated: "border-red-300 bg-red-50 text-red-950",
  completed: "border-green-300 bg-green-50 text-green-950",
}

const CUSTOMER_TYPE_KEYS: Record<string, string> = {
  individual: "individual",
  corporate_domestic: "corporateDomestic",
  corporate_foreign: "corporateForeign",
  trust: "trust",
  partnership: "partnership",
  npo: "npo",
  government: "government",
  foreign_legal_arrangement: "foreignLegalArrangement",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

function identityValue(customer: { attributes: Record<string, unknown> }, key: string) {
  const value = customer.attributes?.[key]
  return typeof value === "string" ? value : ""
}

function formatCountry(code: string, locale: string) {
  try { return new Intl.DisplayNames([locale], { type: "region" }).of(code) ?? code } catch { return code }
}

export function CustomerDetailPage() {
  const { t, i18n } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const [refreshVersion, setRefreshVersion] = useState(0)
  const requestKey = `${id ?? ""}:${refreshVersion}`
  const { data: customer, loading, error } = useApi(
    useCallback(() => api.customers.get(id!), [id]),
    requestKey,
  )
  const { data: scores, loading: scoresLoading } = useApi(
    useCallback(() => api.customers.scoreHistory(id!), [id]),
    requestKey,
  )
  const { data: durableScreening, loading: durableScreeningLoading, error: durableScreeningError } = useApi(
    useCallback(() => api.customers.screeningResults(id!), [id]),
    requestKey,
  )
  const { data: screeningRuns } = useApi(
    useCallback(() => api.customers.screeningRuns(id!), [id]),
    requestKey,
  )
  const { data: investigation, loading: investigationLoading, error: investigationError } = useApi(
    useCallback(() => api.customers.investigation(id!), [id]),
    requestKey,
  )
  const { data: cddRules } = useApi(
    useCallback(() => api.rules.list({ type: "CDD_WEIGHT", activeOnly: true }), []),
  )
  const { data: kyc } = usePolicy("kyc_required_fields")
  const { data: eddPolicy } = usePolicy("edd")
  const { data: eddEvents } = useApi(
    useCallback(() => api.customers.eddEvents(id!), [id]),
    requestKey,
  )
  const { data: identityHistory } = useApi(
    useCallback(() => api.customers.identityHistory(id!), [id]),
    requestKey,
  )
  const [scoring, setScoring] = useState(false)
  const [screening, setScreening] = useState(false)
  const [screenResult, setScreenResult] = useState<ScreenResult | null>(null)
  const [screenError, setScreenError] = useState<string | null>(null)
  const [mutationError, setMutationError] = useState<string | null>(null)
  const [selectedRuleSet, setSelectedRuleSet] = useState("")
  const [scoreRationale, setScoreRationale] = useState("")
  const [scoreConfirmation, setScoreConfirmation] = useState(false)
  const [reviewingResult, setReviewingResult] = useState<string | null>(null)
  const [reviewReason, setReviewReason] = useState<Record<string, string>>({})
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [identityDraft, setIdentityDraft] = useState<Record<string, string>>({})
  const [countryDraft, setCountryDraft] = useState("")
  const [eddRationale, setEddRationale] = useState("")
  const [eddCaseID, setEddCaseID] = useState("")
  const [eddSubmitting, setEddSubmitting] = useState(false)
  const [eddError, setEddError] = useState<string | null>(null)
  const statusRef = useRef<HTMLSelectElement>(null)

  const effectiveRuleSet = selectedRuleSet || cddRules?.data?.[0]?.name || ""
  const { required: requiredIdentityFields, fields: identityFields } = identityRequirements(kyc?.document, customer?.customer_type ?? "")

  // The draft is seeded from the record at the moment editing starts, so a
  // background refresh cannot overwrite what the operator is typing.
  function startEditing() {
    if (!customer) return
    setIdentityDraft(Object.fromEntries(identityFields.map((field) => [field, identityValue(customer, field)])))
    setCountryDraft(customer.country_code)
    setEditing(true)
  }

  async function handleScore() {
    if (!id) return
    if (!effectiveRuleSet) {
      setMutationError(t("customerDetail.riskAssessment.ruleRequired"))
      return
    }
    if (!scoreRationale.trim()) {
      setMutationError(t("customerDetail.riskAssessment.rationaleRequired"))
      return
    }
    if (!scoreConfirmation) {
      setScoreConfirmation(true)
      return
    }
    setScoring(true)
    setMutationError(null)
    try {
      await api.customers.score(id, effectiveRuleSet, { rationale: scoreRationale.trim(), confirmed: true })
      setScoreConfirmation(false)
      setRefreshVersion((version) => version + 1)
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : String(err))
    } finally {
      setScoring(false)
    }
  }

  async function handleEDDAction(action: "complete" | "reopen") {
    if (!id) return
    // The server enforces these; validating here keeps the operator from
    // losing a typed rationale to a 400.
    if (!eddRationale.trim()) {
      setEddError(t("customerDetail.investigation.eddRationaleRequired"))
      return
    }
    if (action === "complete" && eddPolicy?.document?.completion?.requires_case_link && !eddCaseID.trim()) {
      setEddError(t("customerDetail.investigation.eddCaseRequired"))
      return
    }
    setEddSubmitting(true)
    setEddError(null)
    try {
      await api.customers.eddAction(id, action, {
        rationale: eddRationale.trim(),
        ...(action === "complete" && eddCaseID.trim() ? { case_id: eddCaseID.trim() } : {}),
      })
      setEddRationale("")
      setEddCaseID("")
      setRefreshVersion((version) => version + 1)
    } catch (err) {
      setEddError(err instanceof Error ? err.message : String(err))
    } finally {
      setEddSubmitting(false)
    }
  }

  async function handleReview(result: ScreeningResultRecord, status: ScreeningResultStatus) {
    if (status === "FALSE_POSITIVE" && !reviewReason[result.id]?.trim()) {
      setScreenError(t("customerDetail.screening.table.reasonRequired"))
      return
    }
    setReviewingResult(result.id)
    setScreenError(null)
    try {
      await api.customers.reviewScreeningResult(result.id, {
        status,
        false_positive_reason: reviewReason[result.id]?.trim(),
        rationale: reviewReason[result.id]?.trim(),
        expected_version: result.version,
      })
      setRefreshVersion((version) => version + 1)
    } catch (err) {
      setScreenError(err instanceof Error ? err.message : String(err))
    } finally {
      setReviewingResult(null)
    }
  }

  async function handleScreen() {
    if (!id) return
    setScreening(true)
    setScreenError(null)
    try {
      const result = await api.customers.screen(id, [])
      setScreenResult(result)
    } catch (err) {
      setScreenError(err instanceof Error ? err.message : String(err))
    } finally {
      setScreening(false)
    }
  }

  async function handleSave() {
    if (!id || !customer) return
    setSaving(true)
    setMutationError(null)
    try {
      await api.customers.update(id, {
        country_code: countryDraft.trim().toUpperCase(),
        status: statusRef.current?.value as Customer["status"],
        // A cleared field is sent as null so the server removes it rather
        // than keeping the previous value.
        identity: Object.fromEntries(Object.entries(identityDraft).map(([field, value]) => [field, value.trim() || null])),
        rationale: t("customerDetail.basicInfo.identityRationale"),
        expected_updated_at: customer.updated_at,
      })
      setEditing(false)
      setRefreshVersion((version) => version + 1)
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
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

  if (error || !customer) {
    return (
      <div className="space-y-4">
        <Link to="/customers" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("customerDetail.backToList")}
        </Link>
        <p role="alert" className="text-destructive">{t("customerDetail.error")}</p>
      </div>
    )
  }

  const edd = investigation?.edd
  const matches = screenResult?.matches ?? []
  const durableMatches = durableScreening?.data ?? []
  const latestRun = screeningRuns?.data?.[0]
  const hasScreeningHit = Boolean(screenResult?.hit || durableMatches.length > 0)
  // A degraded run matched against an incomplete set of lists, so "no hit"
  // here is not evidence that the customer is clear.
  const degradedRun = Boolean(latestRun?.degraded) || durableMatches.some((match) => match.degraded)
  const degradedSources = Array.from(new Set([...(latestRun?.degraded_sources ?? []), ...durableMatches.flatMap((match) => match.degraded_sources ?? [])]))

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/customers" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("customerDetail.back")}
        </Link>
        {identityValue(customer, "name_ja") || identityValue(customer, "name") ? <><h1 className="text-2xl font-bold tracking-tight">{identityValue(customer, "name_ja") || identityValue(customer, "name")}</h1><span className="text-sm text-muted-foreground">{customer.external_id}</span></> : <h1 className="text-2xl font-bold tracking-tight">{customer.external_id}</h1>}
        {customer.risk_tier && (
          <Badge variant={TIER_VARIANT[customer.risk_tier]}>
            {t(`customers.tier.${customer.risk_tier}`)}
          </Badge>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-base">{t("customerDetail.basicInfo.title")}</CardTitle>
            <Button size="sm" variant="ghost" aria-label={editing ? t("customerDetail.basicInfo.cancel") : t("customerDetail.basicInfo.edit")} onClick={() => editing ? setEditing(false) : startEditing()}>
              <Pencil className="h-4 w-4" />
            </Button>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.internalId")}</dt>
                <dd className="font-mono">{customer.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.type")}</dt>
                <dd>
                  {t(`customers.type.${CUSTOMER_TYPE_KEYS[customer.customer_type] ?? customer.customer_type}`, {
                    defaultValue: customer.customer_type,
                  })}
                </dd>
              </div>
              {editing ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  <IdentityFields customerType={customer.customer_type} policy={kyc?.document} values={identityDraft} onChange={(field, value) => setIdentityDraft((current) => ({ ...current, [field]: value }))} idPrefix="customer-detail-identity" />
                </div>
              ) : (
                identityFields.map((field) => (
                  <div key={field} className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">
                      {t(`customers.identityField.${field}`, { defaultValue: field })}
                      {requiredIdentityFields.has(field) && <span className="ml-1 text-destructive" title={t("customers.identityField.requiredHint")}>*</span>}
                    </dt>
                    <dd className="text-right">{identityValue(customer, field) || "-"}</dd>
                  </div>
                ))
              )}
              {(customer.kyc_missing_fields?.length ?? 0) > 0 && (
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">{t("customerDetail.basicInfo.kycMissing")}</dt>
                  <dd role="alert" className="text-right text-destructive">{customer.kyc_missing_fields?.map((field) => t(`customers.identityField.${field}`, { defaultValue: field })).join(", ")}</dd>
                </div>
              )}
              {customer.kyc_policy_version && (
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">{t("customerDetail.basicInfo.kycPolicyVersion")}</dt>
                  <dd className="text-right font-mono text-xs">{customer.kyc_policy_version}</dd>
                </div>
              )}
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.countryCode")}</dt>
                <dd>
                  {editing ? (
                    <div className="flex gap-2">
                      <CountrySelect id="customer-detail-country" label={t("customerDetail.basicInfo.countryCode")} value={countryDraft} onChange={setCountryDraft} />
                      <Button size="sm" variant="outline" onClick={handleSave} disabled={saving}>
                        {t("customerDetail.basicInfo.save")}
                      </Button>
                    </div>
                  ) : <span title={customer.country_code}>{formatCountry(customer.country_code, i18n.language)}</span>}
                </dd>
              </div>
              <div className="flex justify-between"><dt className="text-muted-foreground">{t("customerDetail.basicInfo.status")}</dt><dd>{editing ? <select aria-label={t("customerDetail.basicInfo.status")} ref={statusRef} defaultValue={customer.status ?? "active"} className="rounded-md border bg-background px-2 py-1 text-sm"><option value="active">{t("customers.status.active")}</option><option value="dormant">{t("customers.status.dormant")}</option><option value="frozen">{t("customers.status.frozen")}</option><option value="closed">{t("customers.status.closed")}</option></select> : <Badge variant="outline">{t(`customers.status.${customer.status ?? "active"}`, { defaultValue: customer.status ?? "active" })}</Badge>}</dd></div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.products")}</dt>
                <dd>{customer.product_types?.join(", ") || "-"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.createdAt")}</dt>
                <dd>{formatDateTime(customer.created_at, i18n.language)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.updatedAt")}</dt>
                <dd>{formatDateTime(customer.updated_at, i18n.language)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-base">{t("customerDetail.riskAssessment.title")}</CardTitle>
            <div className="flex gap-2">
              <Button size="sm" variant="outline" onClick={handleScreen} disabled={screening}>
                <Search className={`h-4 w-4 ${screening ? "animate-pulse" : ""}`} />
                {t("customerDetail.riskAssessment.screenButton")}
              </Button>
              <Button size="sm" variant="outline" onClick={handleScore} disabled={scoring}>
                <RefreshCw className={`h-4 w-4 ${scoring ? "animate-spin" : ""}`} />
                {scoreConfirmation ? t("customerDetail.riskAssessment.confirmScore") : t("customerDetail.riskAssessment.scoreButton")}
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.riskAssessment.riskScore")}</dt>
                <dd className="text-2xl font-bold">
                  {customer.risk_score != null ? customer.risk_score.toFixed(1) : "-"}
                </dd>
              </div>
              <div className="space-y-1">
                <label htmlFor="cdd-rule-set" className="text-muted-foreground">{t("customerDetail.riskAssessment.ruleSet")}</label>
                <select id="cdd-rule-set" aria-label={t("customerDetail.riskAssessment.ruleSet")} value={effectiveRuleSet} onChange={(event) => { setSelectedRuleSet(event.target.value); setScoreConfirmation(false) }} className="w-full rounded-md border bg-background px-2 py-1">
                  <option value="">{t("customerDetail.riskAssessment.selectRuleSet")}</option>
                  {(cddRules?.data ?? []).map((rule) => <option key={`${rule.name}-${rule.version}`} value={rule.name}>{rule.name} v{rule.version} · {rule.description ?? rule.name}</option>)}
                </select>
              </div>
              <div className="space-y-1">
                <label htmlFor="score-rationale" className="text-muted-foreground">{t("customerDetail.riskAssessment.rationale")}</label>
                <input id="score-rationale" aria-label={t("customerDetail.riskAssessment.rationale")} value={scoreRationale} onChange={(event) => { setScoreRationale(event.target.value); setScoreConfirmation(false) }} className="w-full rounded-md border bg-background px-2 py-1" />
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.riskAssessment.riskTier")}</dt>
                <dd>
                  {customer.risk_tier ? (
                    <Badge variant={TIER_VARIANT[customer.risk_tier]}>
                      {t(`customers.tier.${customer.risk_tier}`)}
                    </Badge>
                  ) : t("customerDetail.riskAssessment.unscored")}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.riskAssessment.lastScored")}</dt>
                <dd>{customer.last_scored_at ? formatDateTime(customer.last_scored_at, i18n.language) : "-"}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>

      {mutationError && <p role="alert" className="text-sm text-destructive">{mutationError}</p>}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("customerDetail.investigation.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          {investigationLoading ? (
            <p role="status" className="text-sm text-muted-foreground">{t("customerDetail.investigation.loading")}</p>
          ) : investigationError ? (
            <p role="alert" className="text-sm text-destructive">{t("customerDetail.investigation.error", { error: investigationError })}</p>
          ) : investigation ? (
            <div className="space-y-4">
              <div className="grid gap-2 text-sm sm:grid-cols-3">
                {Object.entries(investigation.counts ?? {}).map(([key, count]) => (
                  <div key={key} className="rounded-md bg-muted/50 px-3 py-2">
                    <div className="text-muted-foreground">{t(`customerDetail.investigation.count.${key}`, { defaultValue: key })}</div>
                    <div className="text-lg font-semibold">{count}</div>
                  </div>
                ))}
              </div>
              {edd && edd.completion_status !== "not_required" && (
                <div role="status" className={`space-y-2 rounded-md border p-3 text-sm ${EDD_STATUS_CLASS[edd.completion_status]}`}>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-semibold">{t("customerDetail.investigation.eddRequired")}</span>
                    <Badge variant={EDD_STATUS_VARIANT[edd.completion_status]}>{t(`customerDetail.investigation.eddStatus.${edd.completion_status}`)}</Badge>
                    {edd.case_id && <Link to={`/cases/${edd.case_id}`} className="text-primary underline-offset-4 hover:underline">{t("customerDetail.investigation.eddCase")}</Link>}
                  </div>
                  {/* overdue_days, not remaining_days: the latter is clamped at
                      zero, so a window 200 days late read the same as one due
                      today. */}
                  <div>{(edd.overdue_days ?? 0) > 0
                    ? t("customerDetail.investigation.eddOverdue", { days: edd.overdue_days })
                    : t("customerDetail.investigation.eddDueAt", { date: edd.due_at ? formatDateTime(edd.due_at, i18n.language) : "-" })}</div>
                  <div>{t("customerDetail.investigation.eddStage", { stage: edd.current_stage })}</div>
                  <div>{t("customerDetail.investigation.eddElapsed", { days: edd.elapsed_days ?? 0 })}</div>
                  {edd.next_stage && edd.next_stage !== "none" && <div>{t("customerDetail.investigation.eddNextStage", { stage: edd.next_stage, date: edd.next_stage_at ? formatDateTime(edd.next_stage_at, i18n.language) : "-" })}</div>}
                  {edd.completed_at && <div>{t("customerDetail.investigation.eddCompletedAt", { date: formatDateTime(edd.completed_at, i18n.language) })}</div>}
                  {edd.policy_version && <div className="text-xs opacity-80">{t("customerDetail.investigation.eddPolicyVersion", { version: edd.policy_version })}</div>}

                  <div className="grid gap-2 sm:grid-cols-2">
                    <div>
                      <label htmlFor="edd-rationale" className="mb-1 block text-xs font-medium">{t("customerDetail.investigation.eddRationale")}</label>
                      <input id="edd-rationale" aria-required="true" value={eddRationale} onChange={(event) => setEddRationale(event.target.value)} className="w-full rounded-md border bg-background px-2 py-1 text-sm" />
                    </div>
                    <div>
                      <label htmlFor="edd-case-id" className="mb-1 block text-xs font-medium">
                        {t("customerDetail.investigation.eddCaseId")}
                        {eddPolicy?.document?.completion?.requires_case_link && <span className="ml-1 text-destructive">*</span>}
                      </label>
                      <input id="edd-case-id" aria-required={Boolean(eddPolicy?.document?.completion?.requires_case_link)} value={eddCaseID} onChange={(event) => setEddCaseID(event.target.value)} className="w-full rounded-md border bg-background px-2 py-1 text-sm" />
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {edd.completion_status === "completed" ? (
                      <Button size="sm" variant="outline" disabled={eddSubmitting} onClick={() => handleEDDAction("reopen")}>{t("customerDetail.investigation.eddReopen")}</Button>
                    ) : (
                      <Button size="sm" variant="outline" disabled={eddSubmitting} onClick={() => handleEDDAction("complete")}>{t("customerDetail.investigation.eddComplete")}</Button>
                    )}
                  </div>
                  {eddError && <p role="alert" className="text-sm text-destructive">{eddError}</p>}
                  {(eddEvents?.length ?? 0) > 0 && (
                    <div>
                      <h3 className="mb-1 text-xs font-semibold">{t("customerDetail.investigation.eddEvents")}</h3>
                      <ul className="space-y-1 text-xs">
                        {eddEvents?.map((event) => (
                          <li key={event.id}>
                            {formatDateTime(event.created_at, i18n.language)} · {t(`customerDetail.investigation.eddEventType.${event.event_type}`, { defaultValue: event.event_type })} · {event.actor}
                            {event.rationale ? ` — ${event.rationale}` : ""}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              )}
              {(investigation.timeline?.length ?? 0) > 0 && (
                <div>
                  <h3 className="mb-2 text-sm font-semibold">{t("customerDetail.investigation.timeline")}</h3>
                  <ul className="space-y-1 text-sm">
                    {investigation.timeline?.slice(0, 8).map((event) => (
                      <li key={event.id} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
                        <span><Badge variant="secondary" className="mr-2">{event.kind}</Badge>{event.summary || event.entity_id}</span>
                        <span className="shrink-0 text-xs text-muted-foreground">{formatDateTime(event.created_at, i18n.language)}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {investigation.partial_failures?.length > 0 && (
                <p role="alert" className="text-sm text-destructive">{t("customerDetail.investigation.partial", { sources: investigation.partial_failures.join(", ") })}</p>
              )}
              <p className="text-xs text-muted-foreground">{t("customerDetail.investigation.freshness", { time: formatDateTime(investigation.freshness, i18n.language) })}</p>
            </div>
          ) : null}
        </CardContent>
      </Card>

      {(screenResult || latestRun || durableScreening) && (
        <Card className={screenResult?.hit || durableMatches.length > 0 ? "border-red-200" : "border-green-200"}>
          <CardHeader>
            <CardTitle className="text-base">
              {t("customerDetail.screening.title")}
              <Badge variant={hasScreeningHit ? "destructive" : "low"} className="ml-2">
                {hasScreeningHit ? t("customerDetail.screening.hit") : t("customerDetail.screening.noHit")}
              </Badge>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              {screenResult
                ? t("customerDetail.screening.summary", {
                    count: screenResult.lists_checked,
                    time: formatDateTime(screenResult.screened_at, i18n.language),
                  })
                : latestRun
                  ? t("customerDetail.screening.durableSummary", { count: latestRun.result_count, time: formatDateTime(latestRun.created_at, i18n.language) })
                  : t("customerDetail.screening.loading")}
            </p>
            {degradedRun && (
              <p role="alert" className="mt-2 rounded-md border border-destructive/50 bg-destructive/5 p-2 text-sm text-destructive">
                {t("customerDetail.screening.degraded")}
                {degradedSources.length > 0 ? ` ${t("customerDetail.screening.degradedSources", { sources: degradedSources.join(", ") })}` : ""}
              </p>
            )}
            {screenError && <p role="alert" className="mt-2 text-sm text-destructive">{t("customerDetail.screening.error", { error: screenError })}</p>}
            {durableScreeningError && <p role="alert" className="mt-2 text-sm text-destructive">{t("customerDetail.screening.durableError", { error: durableScreeningError })}</p>}
            {durableScreeningLoading && !screenResult && <p role="status" className="mt-2 text-sm text-muted-foreground">{t("customerDetail.screening.loading")}</p>}
            {(matches.length > 0 || durableMatches.length > 0) && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("customerDetail.screening.table.list")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.matchedName")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.similarity")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.type")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.source")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.status")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.action")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(matches.length > 0 ? matches : durableMatches).map((m, i) => (
                    <TableRow key={"id" in m ? m.id : i}>
                      <TableCell className="font-mono text-xs">{m.list_id}</TableCell>
                      <TableCell>{m.matched_name}</TableCell>
                      <TableCell>{(m.similarity * 100).toFixed(1)}%</TableCell>
                      <TableCell>{m.list_type}</TableCell>
                      <TableCell>{"source" in m ? m.source : m.run_id ?? "durable"}</TableCell>
                      <TableCell>{"status" in m ? m.status : t("customerDetail.screening.transient")}</TableCell>
                      <TableCell>
                        {"status" in m && (m.status === "NEW" || m.status === "REVIEWING") ? (
                          <div className="flex flex-wrap items-center gap-1">
                            <input aria-label={t("customerDetail.screening.reasonLabel")} value={reviewReason[m.id] ?? ""} onChange={(event) => setReviewReason((current) => ({ ...current, [m.id]: event.target.value }))} placeholder={t("customerDetail.screening.reasonPlaceholder")} className="w-36 rounded border px-1 py-0.5 text-xs" />
                            {m.status === "NEW" && <Button size="sm" variant="outline" onClick={() => handleReview(m, "REVIEWING")} disabled={reviewingResult === m.id}>{t("customerDetail.screening.startReview")}</Button>}
                            {m.status === "REVIEWING" && <>
                              <Button size="sm" variant="outline" onClick={() => handleReview(m, "TRUE_POSITIVE")} disabled={reviewingResult === m.id}>{t("customerDetail.screening.truePositive")}</Button>
                              <Button size="sm" variant="outline" onClick={() => handleReview(m, "FALSE_POSITIVE")} disabled={reviewingResult === m.id}>{t("customerDetail.screening.falsePositive")}</Button>
                            </>}
                          </div>
                        ) : "-"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}

      {customer.attributes && Object.keys(customer.attributes).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("customerDetail.attributes.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-2 text-sm md:grid-cols-2">
              {Object.entries(customer.attributes).map(([key, value]) => (
                <div key={key} className="flex justify-between rounded-md bg-muted/50 px-3 py-2">
                  <dt className="text-muted-foreground">{key}</dt>
                  <dd>{typeof value === "string" ? value : JSON.stringify(value)}</dd>
                </div>
              ))}
            </dl>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("customerDetail.identityHistory.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          {identityHistory?.data?.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("customerDetail.identityHistory.table.changedAt")}</TableHead>
                  <TableHead>{t("customerDetail.identityHistory.table.actor")}</TableHead>
                  <TableHead>{t("customerDetail.identityHistory.table.changedFields")}</TableHead>
                  <TableHead>{t("customerDetail.identityHistory.table.rationale")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {identityHistory.data.map((entry) => (
                  <TableRow key={entry.id}>
                    <TableCell className="whitespace-nowrap text-xs">{formatDateTime(entry.created_at, i18n.language)}</TableCell>
                    <TableCell className="font-mono text-xs">{entry.actor}</TableCell>
                    <TableCell className="text-xs">{Object.keys(entry.changed_fields ?? {}).map((field) => t(`customers.identityField.${field}`, { defaultValue: field })).join(", ") || "-"}</TableCell>
                    <TableCell className="text-xs">{entry.rationale || "-"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("customerDetail.identityHistory.empty")}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("customerDetail.scoreHistory.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          {scoresLoading ? (
            <div className="h-32 animate-pulse rounded bg-muted" />
          ) : scores && scores.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("customerDetail.scoreHistory.table.score")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.tier")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.ruleSet")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.version")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.evaluatedAt")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {scores.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-bold">{s.score.toFixed(1)}</TableCell>
                    <TableCell>
                      <Badge variant={TIER_VARIANT[s.tier]}>{t(`customers.tier.${s.tier}`)}</Badge>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{s.rule_set_id}</TableCell>
                    <TableCell>v{s.rule_set_version}</TableCell>
                    <TableCell>{formatDateTime(s.scored_at, i18n.language)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("customerDetail.scoreHistory.empty")}</p>
          )}
        </CardContent>
      </Card>

      {scores && scores.length > 0 && (scores[0].factors?.length ?? 0) > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("customerDetail.scoreFactors.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("customerDetail.scoreFactors.table.axis")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.name")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.observed")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.score")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.weight")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.contribution")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.rule")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.description")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.businessMeaning")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(scores[0].factors ?? []).map((factor) => (
                  <TableRow key={`${factor.axis}-${factor.name}`}>
                    <TableCell>{factor.axis}</TableCell>
                    <TableCell>{factor.name}</TableCell>
                    <TableCell>{factor.observed_value ?? "-"}</TableCell>
                    <TableCell>{factor.score.toFixed(1)}</TableCell>
                    <TableCell>{factor.weight == null ? "-" : factor.weight.toFixed(2)}</TableCell>
                    <TableCell>{factor.contribution == null ? "-" : factor.contribution.toFixed(2)}</TableCell>
                    <TableCell>{factor.rule ?? (factor.fallback ? t("customerDetail.scoreFactors.fallback") : "-")}</TableCell>
                    <TableCell>{factor.description}</TableCell>
                    <TableCell>{factor.business_meaning ?? "-"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <p className="mt-2 text-xs text-muted-foreground">{t("customerDetail.scoreFactors.total", { score: scores[0].score.toFixed(2) })}</p>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
