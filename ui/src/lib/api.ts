const BASE = "/api/v1"

// getCookie reads the non-HttpOnly csrf_token cookie set at login, so it can
// be echoed back per the Double Submit Cookie CSRF scheme (the authentication model §2).
function getCookie(name: string): string | null {
  const match = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`))
  return match ? decodeURIComponent(match[1]) : null
}

// ApiError carries the server's stable `error_code` (api/internal/apierr)
// alongside the human-readable message, so callers can branch on `code`
// (Contract Stability) and translate via errors.{code} instead of matching
// on message wording, which may change or be translated server-side.
export class ApiError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const method = (init?.method ?? "GET").toUpperCase()
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init?.headers as Record<string, string> | undefined),
  }
  if (method !== "GET" && method !== "HEAD") {
    const csrfToken = getCookie("csrf_token")
    if (csrfToken) headers["X-CSRF-Token"] = csrfToken
  }

  const res = await fetch(`${BASE}${path}`, { ...init, headers })
  if (!res.ok) {
    const body = await res.text()
    let message = body
    let code: string | undefined
    try {
      const parsed: unknown = JSON.parse(body)
      if (parsed && typeof parsed === "object") {
        const { error, error_code } = parsed as { error?: unknown; error_code?: unknown }
        if (typeof error === "string") message = error
        if (typeof error_code === "string") code = error_code
      }
    } catch {
      // Response body was not JSON (e.g. a proxy error page); fall back to
      // the raw text as the message.
    }
    throw new ApiError(message, res.status, code)
  }
  return res.json()
}

async function requestBlob(path: string, init?: RequestInit): Promise<Blob> {
  const method = (init?.method ?? "GET").toUpperCase()
  const headers: Record<string, string> = {
    ...(init?.headers as Record<string, string> | undefined),
  }
  if (method !== "GET" && method !== "HEAD") {
    const csrfToken = getCookie("csrf_token")
    if (csrfToken) headers["X-CSRF-Token"] = csrfToken
  }
  const res = await fetch(`${BASE}${path}`, { ...init, headers })
  if (!res.ok) {
    const body = await res.text()
    let message = body
    let code: string | undefined
    try {
      const parsed: unknown = JSON.parse(body)
      if (parsed && typeof parsed === "object") {
        const errorBody = parsed as { error?: unknown; error_code?: unknown }
        if (typeof errorBody.error === "string") message = errorBody.error
        if (typeof errorBody.error_code === "string") code = errorBody.error_code
      }
    } catch {
      // Keep a proxy/plain-text error useful to the operator.
    }
    throw new ApiError(message, res.status, code)
  }
  return res.blob()
}

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

export interface ScreeningListFreshnessStat {
  list_id: string
  list_type: string
  stale_days: number
  needs_operational_alert: boolean
  operational_state?: "never_imported" | "ready" | "stale" | "unreadable" | "failed" | "unavailable"
  last_attempt_at?: string
  last_success_at?: string
  age_seconds?: number
  diagnostic?: string
}

export interface DashboardStats {
  customers_by_risk_tier: Record<string, number>
  total_customers: number
  alerts_by_status: Record<string, number>
  alerts_by_severity: Record<string, number>
  total_alerts: number
  cases_by_status: Record<string, number>
  total_cases: number
  recent_transactions: number
  recent_transactions_window_hours: number
  screening_list_freshness?: ScreeningListFreshnessStat[]
  // Every source the readiness policy marks required is usable. False means
  // customers are being screened against an incomplete picture.
  screening_ready?: boolean
  screening_degraded_sources?: string[]
}

export type RiskTier = "low" | "medium" | "high"
export type AlertSeverity = "low" | "medium" | "high" | "critical"
export type AlertStatus = "open" | "investigating" | "escalated" | "closed_true_positive" | "closed_false_positive"
export type CaseStatus =
  | "open"
  | "new"
  | "investigating"
  | "escalated"
  | "closed"
  | "reopened"
  | "str_filed"
export type CasePriority = "low" | "medium" | "high" | "critical"

export interface Customer {
  id: string
  external_id: string
  customer_type: string
  country_code: string
  product_types: string[]
  status?: "active" | "dormant" | "frozen" | "closed"
  attributes: Record<string, unknown>
  risk_score?: number
  risk_tier?: RiskTier
  last_scored_at?: string
  // Required identity attributes this record does not carry, per the
  // kyc_required_fields policy for its type. Recomputed on read and absent
  // when nothing is missing.
  kyc_missing_fields?: string[]
  kyc_policy_version?: string
  created_at: string
  updated_at: string
  // EDD 3段階エスカレーション（the case-management workflow §EDD未実施継続時の段階的
  // 措置）。edd_requested_at が未設定なら EDD 要求状態にない。
  edd_requested_at?: string
  edd_stage1_last_sent_at?: string
  edd_stage2_notified_at?: string
  edd_stage3_notified_at?: string
}

export interface Alert {
  id: string
  customer_id: string
  scenario_id: string
  severity: AlertSeverity
  status: AlertStatus
  score: number
  description: string
  transaction_ids: string[]
  detected_at: string
  resolved_at?: string
  resolved_by?: string
  created_at: string
  updated_at: string
  assigned_to?: string
  assigned_team?: string
  due_at?: string
  disposition?: string
  disposition_rationale?: string
}

export interface Case {
  id: string
  customer_id: string
  alert_ids: string[]
  status: CaseStatus
  priority: CasePriority
  assigned_to?: string
  assigned_team?: string
  due_at?: string
  summary: string
  notes?: CaseNote[]
  reopen_reason?: string
  related_case_ids?: string[]
  investigation_disposition?: string
  str_candidate?: boolean
  disposition_rationale?: string
  str_report_id?: string
  str_filed_at?: string
  str_filed_by?: string
  str_filing_channel?: string
  str_destination?: string
  str_external_reference?: string
  created_at: string
  updated_at: string
  closed_at?: string
}

export interface CaseNote {
  id: string
  author: string
  content: string
  created_at: string
}

// RelatedCase pairs a case with how it was linked (the case-management workflow
// §ケース間の関連付け: 同一顧客の自動抽出 or 手動リンク).
export interface RelatedCase {
  case: Case
  link_type: "auto" | "manual"
  relationship?: CaseRelationship
}

export interface CaseRelationship {
  id: string
  case_id: string
  related_case_id: string
  relationship_type: string
  rationale: string
  created_by: string
  created_at: string
  active: boolean
  removed_by?: string
  removed_at?: string
  removal_reason?: string
  source: "auto" | "manual"
}

export interface CaseEvent {
  id: string
  case_id: string
  event_type: string
  actor: string
  reason?: string
  before?: Record<string, unknown>
  after?: Record<string, unknown>
  related_alert_ids?: string[]
  related_case_ids?: string[]
  related_report_ids?: string[]
  correlation_id?: string
  created_at: string
}

export interface CaseEvidence {
  id: string
  case_id: string
  description: string
  source: string
  evidence_type: string
  collected_at: string
  collected_by: string
  integrity_hash?: string
  version: number
  created_at: string
}

export interface CaseChecklistItem {
  id: string
  case_id: string
  key: string
  label: string
  completed: boolean
  completed_by?: string
  completed_at?: string
  version: number
  created_at: string
  updated_at: string
}

export interface CaseWorkItem {
  id: string
  case_id: string
  title: string
  description?: string
  status: string
  assigned_to?: string
  due_at?: string
  completed_by?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export interface CaseFile {
  case: Case
  events: CaseEvent[]
  event_pagination?: PaginationMeta
  evidence: CaseEvidence[]
  checklist: CaseChecklistItem[]
  work_items: CaseWorkItem[]
  relationships: CaseRelationship[]
}

export interface OperatorDirectory {
  users: { id: string; email: string; role: string }[]
  teams: string[]
}

export interface AlertDecisionEvent {
  id: string
  alert_id: string
  from_status: AlertStatus
  to_status: AlertStatus
  outcome: string
  rationale: string
  actor: string
  supersedes_id?: string
  created_at: string
}

export interface Transaction {
  id: string
  customer_id: string
  external_id: string
  amount: number
  currency: string
  direction: "inbound" | "outbound" | "internal"
  counterparty_id?: string
  counterparty_country?: string
  channel?: string
  account_id?: string
  counterparty?: Record<string, unknown>
  metadata?: Record<string, unknown>
  idempotency_key?: string
  travel_rule_applicable?: boolean
  travel_rule_evidence?: Record<string, unknown>
  travel_rule_not_applicable_reason?: string
  executed_at: string
  created_at: string
}

export interface ScoreRecord {
  id: string
  customer_id: string
  score: number
  tier: RiskTier
  factors: Factor[]
  rule_set_id: string
  rule_set_version: number
  rule_set_sha256?: string
  rationale?: string
  actor?: string
  override_evidence?: Record<string, unknown>
  scored_at: string
}

export interface Factor {
	name: string
	axis: string
	score: number
	description: string
	business_meaning?: string
	weight?: number
	contribution?: number
	observed_value?: string
	rule?: string
	fallback?: boolean
}

export interface AuditEntry {
  id: number
  user_id: string
  action: string
  resource_type: string
  resource_id: string
  details?: Record<string, string>
  ip_address?: string
  user_agent?: string
  created_at: string
}

// AuditListParams mirrors ALD-001's filter axes (period, operation
// category, actor, target resource) plus cursor pagination, shared by
// audit.list and audit.export so the export preserves whatever filter is
// currently applied to the list (ALD-004).
export interface AuditListParams {
  resourceType?: string
  resourceId?: string
  userId?: string
  actionCategory?: string
  since?: string
  until?: string
  cursor?: string
  limit?: number
}

function buildAuditQuery(params?: AuditListParams): string {
  const qs = new URLSearchParams()
  if (params?.resourceType) qs.set("resource_type", params.resourceType)
  if (params?.resourceId) qs.set("resource_id", params.resourceId)
  if (params?.userId) qs.set("user_id", params.userId)
  if (params?.actionCategory) qs.set("action_category", params.actionCategory)
  if (params?.since) qs.set("since", params.since)
  if (params?.until) qs.set("until", params.until)
  if (params?.cursor) qs.set("cursor", params.cursor)
  if (params?.limit) qs.set("limit", String(params.limit))
  return qs.toString()
}

export type WebhookEventType =
  | "alert.created" | "alert.resolved"
  | "case.created" | "case.updated" | "case.closed"
  | "str.created" | "score.changed" | "screening.match" | "screening_true_positive"
  | "edd_required" | "transaction_restriction_recommended" | "relationship_decline_recommended"

export interface Webhook {
  id: string
  url: string
  events: WebhookEventType[]
  secret?: string
  active: boolean
  created_at: string
  updated_at: string
}

export interface WebhookDelivery {
  id: string
  webhook_id: string
  event: WebhookEventType
  payload: string
  status_code: number
  success: boolean
  error?: string
  created_at: string
  event_id: string
  attempt_count: number
  next_attempt_at?: string
}

export interface WebhookDLQEntry {
  id: string
  webhook_id: string
  event_id: string
  event: WebhookEventType
  payload: string
  attempt_count: number
  last_error?: string
  failed_at: string
  reprocessed_at?: string
}

export interface STRReport {
  id: string
  alert_id: string
  customer_id: string
  case_id?: string
  report_type: string
  status: string
  suspicious_point: string
  transaction_ids: string[]
  transaction_snapshot: {
    id: string
    external_id: string
    amount: number
    currency: string
    direction: string
    counterparty_id?: string
    counterparty_country?: string
    channel?: string
    executed_at: string
    created_at: string
  }[]
  total_amount: number
  currency: string
  created_at: string
  updated_at: string
  submitted_at?: string
  created_by: string
  submitted_by?: string
  submission_evidence?: string
}

export interface BacktestResult {
  backtest_id: string
  total_transactions: number
  total_customers: number
  total_alerts: number
  scenario_results: BacktestScenarioResult[]
  execution_time_ms: number
}

export type BacktestJobStatus = "queued" | "running" | "completed" | "failed" | "cancelled"
export interface BacktestJob {
  id: string
  status: BacktestJobStatus
  from: string
  to: string
  customer_ids?: string[]
  scenario_ids?: string[]
  baseline_rule_set_id: string
  candidate_rule_set_id: string
  progress: number
  processed_customers: number
  total_customers: number
  eta_seconds?: number
  baseline?: BacktestResult
  candidate?: BacktestResult
  delta?: BacktestResult
  error?: string
  created_at: string
  updated_at: string
  metadata?: {
    rationale: string
    cohort_preview: Record<string, unknown>
    baseline_snapshot: Record<string, unknown>
    candidate_snapshot: Record<string, unknown>
    rerun_of?: string
  }
}

// A customer the candidate rule set starts alerting on (added), stops
// alerting on (removed), or treats identically (unchanged). `mixed` only
// appears in the aggregate: added by one scenario and removed by another.
export type BacktestDeltaKind = "added" | "removed" | "unchanged" | "mixed"

export interface BacktestAffectedCustomer {
  job_id: string
  scenario_id: string
  customer_id: string
  delta_kind: Exclude<BacktestDeltaKind, "mixed">
}

export interface AffectedBacktestCustomersPage extends PaginatedResponse<string> {
  delta_kinds?: Record<string, BacktestDeltaKind>
  rows?: BacktestAffectedCustomer[]
}

export interface BacktestScenarioResult {
  scenario_id: string
  alerts_generated: number
  high_severity_count: number
  medium_severity_count: number
  low_severity_count: number
  affected_customer_ids: string[]
  added_customer_ids?: string[]
  removed_customer_ids?: string[]
}

export type Role = "admin" | "analyst" | "viewer"

export interface APIKey {
  id: string
  name: string
  role: Role
  active: boolean
  created_at: string
  last_used?: string
}

export interface APIKeyCreateResponse {
  id: string
  name: string
  role: Role
  key: string
}

export interface ConfigValidationResult {
  valid: boolean
  errors: { field: string; message: string }[]
}

export interface ScreenMatch {
  list_id: string
  entry_id: string
  matched_name: string
  similarity: number
  list_type: string
  source: string
}

export interface ScreenResult {
  customer_id: string
  hit: boolean
  matches: ScreenMatch[]
  lists_checked: number
  screened_at: string
  run_id?: string
  result_ids?: string[]
}

export type ScreeningResultStatus = "NEW" | "REVIEWING" | "TRUE_POSITIVE" | "FALSE_POSITIVE"

export interface ScreeningResultRecord {
  id: string
  customer_id: string
  list_id: string
  list_type: string
  entry_id: string
  matched_name: string
  similarity: number
  status: ScreeningResultStatus
  false_positive_reason?: string
  reviewed_by?: string
  reviewed_at?: string
  screened_at: string
  created_at: string
  run_id?: string
  suppressed: boolean
  suppression_reason?: string
  match_evidence?: Record<string, unknown>
  case_id?: string
  // A required watchlist source was not ready when this run started, so the
  // absence of a hit is not evidence that the customer is clear.
  degraded?: boolean
  degraded_sources?: string[]
  version: number
  updated_at: string
}

export interface ScreeningRun {
  id: string
  customer_id: string
  list_ids: string[]
  config_digests: Record<string, string>
  status: "running" | "completed" | "failed" | "partial"
  result_count: number
  error?: string
  actor: string
  degraded?: boolean
  degraded_sources?: string[]
  started_at: string
  completed_at?: string
  created_at: string
}

export interface ScreeningSourceStatus {
  list_id: string
  list_type: string
  configured: boolean
  operational_state: "never_imported" | "ready" | "stale" | "unreadable" | "failed" | "unavailable"
  last_attempt_at?: string
  last_failure_at?: string
  last_success_at?: string
  age_seconds?: number
  freshness_threshold_seconds: number
  consecutive_failures: number
  diagnostic?: string
}

export interface ScreeningSourceDirectory {
  data: ScreeningSourceStatus[]
  configured_count: number
  ready_count: number
  unready_count: number
  // screening_ready only counts sources the readiness policy marks required,
  // so a stale optional source (pep_provider by default) leaves it true.
  screening_ready: boolean
  degraded_sources?: string[]
  policy_version?: string
}

export interface PendingEvaluation {
  id: string
  customer_id: string
  transaction_ids: string[]
  alert_ids?: string[]
  status: "PENDING_REVIEW" | "PROCESSING" | "RESOLVED" | "FAILED"
  reason: string
  batch_run_id?: string
  retry_count: number
  resolved_at?: string
  last_attempt_at?: string
  next_retry_at?: string
  escalated_at?: string
  version: number
  created_at: string
  updated_at: string
}

export interface PendingEvaluationHistoryEntry {
  id: string
  pending_evaluation_id: string
  from_status: PendingEvaluation["status"]
  to_status: PendingEvaluation["status"]
  action: string
  reason: string
  actor: string
  retry_count: number
  created_at: string
}

export interface ScreeningResultHistoryEntry {
  id: string
  screening_result_id: string
  from_status: ScreeningResultStatus
  to_status: ScreeningResultStatus
  rationale: string
  actor: string
  version: number
  created_at: string
}

export interface ScoreExplanation {
  score: ScoreRecord
  total_reconciled: number
  rule_set_id: string
  rule_set_sha256: string
  priority: string
  deterministic: boolean
}

export type EDDCompletionStatus = "not_required" | "open" | "overdue" | "escalated" | "completed"

export interface EDDPanel {
  required: boolean
  requested_at?: string
  stage1_last_sent_at?: string
  stage2_notified_at?: string
  stage3_notified_at?: string
  completed_at?: string
  closed_at?: string
  close_reason?: string
  current_stage: string
  elapsed_days?: number
  // remaining_days is clamped at zero and cannot express lateness; read
  // overdue_days instead, which counts whole days past the due boundary.
  remaining_days?: number
  overdue_days?: number
  due_at?: string
  next_stage?: string
  next_stage_at?: string
  completion_status: EDDCompletionStatus
  case_id?: string
  policy_version?: string
}

export type EDDEventType = "requested" | "stage_escalated" | "completed" | "reopened" | "closed_on_downgrade"

export interface EDDEvent {
  id: string
  customer_id: string
  event_type: EDDEventType
  stage?: string
  rationale?: string
  case_id?: string
  actor: string
  policy_version?: string
  created_at: string
}

export interface CustomerIdentityHistoryEntry {
  id: string
  customer_id: string
  changed_fields: Record<string, unknown>
  actor: string
  rationale: string
  created_at: string
}

export interface CustomerInvestigation {
  customer: Customer
  counts: Record<string, number>
  transactions: Transaction[]
  alerts: Alert[]
  cases: Case[]
  screening_results: ScreeningResultRecord[]
  score_history: ScoreRecord[]
  timeline?: Array<{ id: string; kind: string; entity_id: string; summary: string; created_at: string }>
  edd?: EDDPanel
  pagination?: Record<string, PaginationMeta>
  freshness: string
  partial_failures: string[]
}

function normalizeScreenResult(result: ScreenResult): ScreenResult {
  return {
    ...result,
    matches: result.matches ?? [],
  }
}

function normalizeBacktestResult(result: BacktestResult): BacktestResult {
  return {
    ...result,
    scenario_results: (result.scenario_results ?? []).map((scenario) => ({
      ...scenario,
      affected_customer_ids: scenario.affected_customer_ids ?? [],
    })),
  }
}

function normalizeBacktestJob(job: BacktestJob): BacktestJob {
  return {
    ...job,
    baseline: job.baseline ? normalizeBacktestResult(job.baseline) : job.baseline,
    candidate: job.candidate ? normalizeBacktestResult(job.candidate) : job.candidate,
    delta: job.delta ? normalizeBacktestResult(job.delta) : job.delta,
  }
}

export interface BatchScoreResult {
  customer_id: string
  score: number
  risk_tier: string
  error?: string
}

export interface BatchScoreResponse {
  total: number
  succeeded: number
  failed: number
  results: BatchScoreResult[]
  duration: string
}

export interface BatchMonitorResult {
  customer_id: string
  alerts_raised: number
  pending_review?: boolean
  error?: string
}

export interface BatchMonitorResponse {
  total: number
  succeeded: number
  failed: number
  queued_for_review: number
  alerts_total: number
  results: BatchMonitorResult[]
  duration: string
}

export interface TargetManifest {
  id: string
  operation: string
  target_mode: "selected" | "filter" | "all"
  customer_ids: string[]
  sample_customer_ids: string[]
  target_count: number
  criteria: string
  token?: string
  status: "preview" | "confirmed" | "consumed" | "expired"
  version: number
  expires_at: string
  created_at: string
  confirmed_at?: string
}

export interface BatchRun {
  id: string
  job_type: string
  operation: string
  status: "running" | "completed" | "failed" | "partial" | "cancelled"
  parameters: Record<string, unknown>
  target_manifest_id: string
  result_counts: Record<string, number>
  customer_outcomes?: Record<string, { customer_id: string; status: "succeeded" | "failed" | "queued" | "error"; alert_ids?: string[]; error?: string; attempt: number; updated_at: string }>
  error?: string
  rerun_of?: string
  started_at: string
  completed_at?: string
  updated_at: string
}

export interface SystemInfo {
  version: string
  components: string[]
  endpoints: number
  features: Record<string, boolean>
}

export interface AuthUser {
  id: string
  email: string
  role: Role
}

export interface User {
  id: string
  email: string
  role: Role
  active: boolean
  created_at: string
  updated_at: string
}

export type RuleType = "TM_SCENARIO" | "CDD_WEIGHT" | "SCREENING_CONFIG" | "COUNTRY_RISK"

export interface RuleDefinition {
  id: string
  type: RuleType
  name: string
  description?: string
  definition: unknown
  version: number
  is_active: boolean
  created_by?: string
  created_at: string
  updated_at: string
}

export interface PaginationMeta {
  next_cursor?: string
  has_more: boolean
}

export interface PaginatedResponse<T> {
  data: T[]
  pagination: PaginationMeta
}

export interface CursorPageParams {
  cursor?: string
  limit?: number
  offset?: number
  sort?: "risk"
}

interface ListAllPagesOptions {
  limit?: number
  maxPages?: number
}

// Follow keyset pages without dropping the response envelope. The caller's
// list function owns all non-pagination filters, so every request in the
// traversal retains the same search/scope semantics.
export async function listAllPages<T>(
  fetchPage: (params?: CursorPageParams) => Promise<PaginatedResponse<T>>,
  options: ListAllPagesOptions = {},
): Promise<PaginatedResponse<T>> {
  const limit = options.limit ?? 200
  // Keep browser callers bounded even when a caller accidentally uses the
  // compatibility helper for an unbounded collection. Queue pages should use
  // the cursor controls directly instead of collecting every page here.
  const maxPages = options.maxPages ?? 50
  const data: T[] = []
  let cursor: string | undefined

  for (let pageNumber = 0; pageNumber < maxPages; pageNumber += 1) {
    const page = await fetchPage(cursor ? { cursor, limit } : { limit })
    data.push(...page.data)

    if (!page.pagination.has_more) {
      return { data, pagination: { has_more: false } }
    }

    const nextCursor = page.pagination.next_cursor
    if (!nextCursor || nextCursor === cursor) {
      throw new Error("paginated response has_more without a usable next_cursor")
    }
    cursor = nextCursor
  }

  throw new Error("paginated response exceeded the maximum page count")
}

function buildCursorQuery(params?: CursorPageParams): URLSearchParams {
  const qs = new URLSearchParams()
  if (params?.cursor) qs.set("cursor", params.cursor)
  if (params?.limit != null) qs.set("limit", String(params.limit))
  if (params?.offset != null) qs.set("offset", String(params.offset))
  if (params?.sort) qs.set("sort", params.sort)
  return qs
}

export interface RuleImportItem {
  type: RuleType
  name: string
  description?: string
  definition: unknown
}

export type WhitelistEntryStatus = "pending_approval" | "active" | "expired" | "revoked"
export type WhitelistReviewDecision = "renewed" | "revoked"

export interface WhitelistEntry {
  id: string
  customer_id: string
  status: WhitelistEntryStatus
  reason: string
  excluded_rule_ids?: string[]
  valid_from: string
  valid_until: string
  requested_by: string
  approved_by?: string
  approved_at?: string
  revoked_by?: string
  revoked_at?: string
  version: number
  created_at: string
  updated_at: string
}

export interface WhitelistReview {
  id: string
  whitelist_entry_id: string
  reviewed_by: string
  decision: WhitelistReviewDecision
  review_notes?: string
  next_review_date?: string
  created_at: string
}

// The five policy documents the server loads at startup (ADR-0016). Every
// threshold, required field, stage schedule and reason code the UI shows must
// come from here rather than from a constant in a page, so the screen cannot
// state a rule the server does not apply.
export type PolicyName =
  | "kyc_required_fields"
  | "edd"
  | "cdd_rule_selection"
  | "travel_rule"
  | "screening_readiness"

export interface PolicyDescriptor {
  name: PolicyName
  schema_version: string
  policy_version: string
  digest: string
  source: "file" | "default"
}

export interface KYCTypeRequirements {
  required: string[]
  recommended?: string[]
}

export interface KYCRequiredFieldsPolicy {
  schema_version: string
  policy_version: string
  enforcement: "warn" | "reject"
  defaults: KYCTypeRequirements
  types: Record<string, KYCTypeRequirements>
}

export interface EDDPolicyStage {
  name: string
  after_days: number
  action: string
  case_priority?: CasePriority
}

export interface EDDPolicyDocument {
  schema_version: string
  policy_version: string
  trigger_tiers: RiskTier[]
  stages: EDDPolicyStage[]
  due_days: number
  completion: { requires_rationale: boolean; requires_case_link: boolean }
  tier_downgrade: string
}

export interface CDDRuleSelectionPolicyDocument {
  schema_version: string
  policy_version: string
  default_rule_set_id: string
  selection_authority: string
  rules: { match: Record<string, string[] | undefined>; rule_set_id: string; priority: number }[]
}

export type CounterpartyType = "vasp" | "unhosted_wallet" | "unknown" | "exempt"

export interface TravelRulePolicyDocument {
  schema_version: string
  policy_version: string
  threshold_amount: number
  threshold_currency: string
  covered_channels: string[]
  covered_directions: string[]
  applicable_counterparty_types: CounterpartyType[]
  required_evidence_fields: Record<string, string[]>
  not_applicable_reasons: string[]
  assertion_authority: string
  incomplete_routing: string
}

export interface ScreeningReadinessPolicyDocument {
  schema_version: string
  policy_version: string
  default_freshness_seconds: number
  mark_runs_degraded: boolean
  gate_screening_runs: boolean
  sources: { list_id: string; required: boolean; freshness_seconds?: number }[]
}

interface PolicyDocumentByName {
  kyc_required_fields: KYCRequiredFieldsPolicy
  edd: EDDPolicyDocument
  cdd_rule_selection: CDDRuleSelectionPolicyDocument
  travel_rule: TravelRulePolicyDocument
  screening_readiness: ScreeningReadinessPolicyDocument
}

export interface PolicyDocument<N extends PolicyName = PolicyName> extends PolicyDescriptor {
  name: N
  document: PolicyDocumentByName[N]
}

export const api = {
  dashboard: () => request<DashboardStats>("/dashboard"),
  policies: {
    list: () => request<{ data: PolicyDescriptor[] }>("/policies"),
    get: <N extends PolicyName>(name: N) =>
      request<PolicyDocument<N>>(`/policies/${encodeURIComponent(name)}`),
  },
  customers: {
    list: (params?: CursorPageParams & { search?: string }) => {
      const qs = buildCursorQuery(params)
      if (params?.search) qs.set("search", params.search)
      const query = qs.toString()
      return request<PaginatedResponse<Customer>>(`/customers${query ? `?${query}` : ""}`)
    },
    listAll: (params?: { search?: string }) =>
      listAllPages((page) => api.customers.list({ ...params, ...page })),
    get: (id: string) => request<Customer>(`/customers/${encodeURIComponent(id)}`),
    create: (data: { external_id: string; customer_type: string; country_code: string; product_types: string[]; attributes: Record<string, unknown>; identity?: Record<string, unknown> }) =>
      request<Customer>("/customers", { method: "POST", body: JSON.stringify(data) }),
    update: (id: string, data: { country_code?: string; status?: Customer["status"]; product_types?: string[]; attributes?: Record<string, unknown>; identity?: Record<string, unknown>; rationale?: string; expected_updated_at?: string }) =>
      request<Customer>(`/customers/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(data) }),
    scoreHistory: (id: string) =>
      request<ScoreRecord[]>(`/customers/${encodeURIComponent(id)}/scores`),
    scoreExplanation: (id: string, scoreId?: string) => {
      const suffix = scoreId ? `/scores/${encodeURIComponent(scoreId)}/explanation` : "/score-explanation"
      return request<ScoreExplanation>(`/customers/${encodeURIComponent(id)}${suffix}`)
    },
    screeningResults: (id: string, params?: CursorPageParams & { status?: ScreeningResultStatus; listId?: string }) => {
      const qs = buildCursorQuery(params)
      if (params?.status) qs.set("status", params.status)
      if (params?.listId) qs.set("list_id", params.listId)
      const query = qs.toString()
      return request<PaginatedResponse<ScreeningResultRecord>>(`/customers/${encodeURIComponent(id)}/screening-results${query ? `?${query}` : ""}`)
    },
    screeningRuns: (id: string, params?: CursorPageParams) => {
      const qs = buildCursorQuery(params)
      qs.set("customer_id", id)
      return request<PaginatedResponse<ScreeningRun>>(`/screening/runs?${qs.toString()}`)
    },
    investigation: (id: string) =>
      request<CustomerInvestigation>(`/customers/${encodeURIComponent(id)}/investigation`),
    // Closing or reopening an EDD window. rationale is required by the edd
    // policy for a reopen and, by default, for a completion too.
    eddAction: (id: string, action: "complete" | "reopen", data: { rationale: string; case_id?: string; expected_updated_at?: string }) =>
      request<EDDPanel>(`/customers/${encodeURIComponent(id)}/edd/${action}`, { method: "POST", body: JSON.stringify(data) }),
    eddEvents: (id: string) =>
      request<EDDEvent[]>(`/customers/${encodeURIComponent(id)}/edd-events`),
    identityHistory: (id: string, params?: CursorPageParams) => {
      const qs = buildCursorQuery(params)
      const query = qs.toString()
      return request<PaginatedResponse<CustomerIdentityHistoryEntry>>(`/customers/${encodeURIComponent(id)}/identity-history${query ? `?${query}` : ""}`)
    },
    score: (id: string, ruleSetId: string, options?: { rule_set_version?: number; rationale?: string; override_evidence?: Record<string, unknown>; confirmed?: boolean }) =>
      request<ScoreRecord>(`/customers/${encodeURIComponent(id)}/score`, {
        method: "POST",
        body: JSON.stringify({ rule_set_id: ruleSetId, ...options }),
      }),
    screen: async (id: string, listIds: string[]) =>
      normalizeScreenResult(await request<ScreenResult>(`/customers/${encodeURIComponent(id)}/screen`, {
        method: "POST",
        body: JSON.stringify({ list_ids: listIds }),
      })),
    reviewScreeningResult: (resultId: string, data: { status: ScreeningResultStatus; false_positive_reason?: string; rationale?: string; expected_version: number }) =>
      request<{ result: ScreeningResultRecord; case_id?: string; case_created: boolean }>(`/screening/results/${encodeURIComponent(resultId)}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
  },
  screening: {
    // Omitting `suppressed` returns both suppressed and unsuppressed hits,
    // which is the historical queue contents; pass false to hide repeat false
    // positives and true to audit what was hidden.
    results: (params?: CursorPageParams & { customerId?: string; status?: ScreeningResultStatus; listId?: string; suppressed?: boolean }) => {
      const qs = buildCursorQuery(params)
      if (params?.customerId) qs.set("customer_id", params.customerId)
      if (params?.status) qs.set("status", params.status)
      if (params?.listId) qs.set("list_id", params.listId)
      if (params?.suppressed != null) qs.set("suppressed", String(params.suppressed))
      const query = qs.toString()
      return request<PaginatedResponse<ScreeningResultRecord>>(`/screening/results${query ? `?${query}` : ""}`)
    },
    getResult: (id: string) => request<ScreeningResultRecord>(`/screening/results/${encodeURIComponent(id)}`),
    history: (id: string) => request<ScreeningResultHistoryEntry[]>(`/screening/results/${encodeURIComponent(id)}/history`),
    sources: (params?: { freshnessThresholdSeconds?: number; sourceIds?: string[] }) => {
      const qs = new URLSearchParams()
      if (params?.sourceIds?.length) qs.set("source_ids", params.sourceIds.join(","))
      if (params?.freshnessThresholdSeconds != null) qs.set("freshness_threshold_seconds", String(params.freshnessThresholdSeconds))
      const query = qs.toString()
      return request<ScreeningSourceDirectory>(`/screening/sources${query ? `?${query}` : ""}`)
    },
  },
  alerts: {
    list: (params?: CursorPageParams & { customerId?: string; status?: string; active?: boolean; terminal?: boolean; assignee?: string; mine?: boolean; team?: string; unassigned?: boolean; severity?: AlertSeverity; scenarioId?: string; search?: string; overdue?: boolean; minAgeDays?: number; maxAgeDays?: number }) => {
      const qs = buildCursorQuery(params)
      if (params?.customerId) qs.set("customer_id", params.customerId)
      if (params?.status) qs.set("status", params.status)
      if (params?.active != null) qs.set("active", String(params.active))
      if (params?.terminal != null) qs.set("terminal", String(params.terminal))
      if (params?.assignee) qs.set("assignee", params.assignee)
      if (params?.mine != null) qs.set("mine", String(params.mine))
      if (params?.team) qs.set("team", params.team)
      if (params?.unassigned != null) qs.set("unassigned", String(params.unassigned))
      if (params?.severity) qs.set("severity", params.severity)
      if (params?.scenarioId) qs.set("scenario_id", params.scenarioId)
      if (params?.search) qs.set("search", params.search)
      if (params?.overdue != null) qs.set("overdue", String(params.overdue))
      if (params?.minAgeDays != null) qs.set("min_age_days", String(params.minAgeDays))
      if (params?.maxAgeDays != null) qs.set("max_age_days", String(params.maxAgeDays))
      const query = qs.toString()
      return request<PaginatedResponse<Alert>>(`/alerts${query ? `?${query}` : ""}`)
    },
    listAll: (params?: { customerId?: string; sort?: "risk"; status?: string; active?: boolean; terminal?: boolean; assignee?: string; mine?: boolean; team?: string; unassigned?: boolean; severity?: AlertSeverity; scenarioId?: string; search?: string; overdue?: boolean; minAgeDays?: number; maxAgeDays?: number }) =>
      listAllPages((page) => api.alerts.list({ ...params, sort: params?.sort ?? "risk", ...page })),
    get: (id: string) => request<Alert>(`/alerts/${encodeURIComponent(id)}`),
    updateStatus: (id: string, status: AlertStatus, expectedUpdatedAt?: string, options?: { rationale?: string; confirm?: boolean; assigned_to?: string; assigned_team?: string; due_at?: string; clear_due_at?: boolean }) =>
      request<Alert>(`/alerts/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify({ status, ...(expectedUpdatedAt ? { expected_updated_at: expectedUpdatedAt } : {}), ...options }),
      }),
    updateQueue: (id: string, options: { assigned_to?: string; assigned_team?: string; due_at?: string; clear_due_at?: boolean; expected_updated_at?: string }) =>
      request<Alert>(`/alerts/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(options) }),
    decisions: (id: string) => request<AlertDecisionEvent[]>(`/alerts/${encodeURIComponent(id)}/decisions`),
    bulkClose: (data: { scenario_id?: string; period_from?: string; period_to?: string; severity?: AlertSeverity; reason: string }) =>
      request<{ closed_count: number; alert_ids: string[] }>("/alerts/bulk-close", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    bulkCase: (data: { alert_ids: string[]; case_id?: string; customer_id?: string; summary?: string }) =>
      request<{ case_id: string; created: boolean }>("/alerts/bulk-case", {
        method: "POST",
        body: JSON.stringify(data),
      }),
  },
  cases: {
    list: (params?: CursorPageParams & { customerId?: string; status?: string; active?: boolean; terminal?: boolean; assignee?: string; mine?: boolean; team?: string; unassigned?: boolean; priority?: CasePriority; disposition?: string; strCandidate?: boolean; search?: string; overdue?: boolean; minAgeDays?: number; maxAgeDays?: number }) => {
      const qs = buildCursorQuery(params)
      if (params?.customerId) qs.set("customer_id", params.customerId)
      if (params?.status) qs.set("status", params.status)
      if (params?.active != null) qs.set("active", String(params.active))
      if (params?.terminal != null) qs.set("terminal", String(params.terminal))
      if (params?.assignee) qs.set("assignee", params.assignee)
      if (params?.mine != null) qs.set("mine", String(params.mine))
      if (params?.team) qs.set("team", params.team)
      if (params?.unassigned != null) qs.set("unassigned", String(params.unassigned))
      if (params?.priority) qs.set("priority", params.priority)
      if (params?.disposition) qs.set("disposition", params.disposition)
      if (params?.strCandidate != null) qs.set("str_candidate", String(params.strCandidate))
      if (params?.search) qs.set("search", params.search)
      if (params?.overdue != null) qs.set("overdue", String(params.overdue))
      if (params?.minAgeDays != null) qs.set("min_age_days", String(params.minAgeDays))
      if (params?.maxAgeDays != null) qs.set("max_age_days", String(params.maxAgeDays))
      const query = qs.toString()
      return request<PaginatedResponse<Case>>(`/cases${query ? `?${query}` : ""}`)
    },
    listAll: (params?: { customerId?: string; sort?: "risk"; status?: string; active?: boolean; terminal?: boolean; assignee?: string; mine?: boolean; team?: string; unassigned?: boolean; priority?: CasePriority; disposition?: string; strCandidate?: boolean; search?: string; overdue?: boolean; minAgeDays?: number; maxAgeDays?: number }) =>
      listAllPages((page) => api.cases.list({ ...params, sort: params?.sort ?? "risk", ...page })),
    get: (id: string) => request<Case>(`/cases/${encodeURIComponent(id)}`),
    create: (data: { customer_id: string; alert_ids: string[]; priority: string; assigned_to?: string; assigned_team?: string; due_at?: string; summary: string }) =>
      request<Case>("/cases", { method: "POST", body: JSON.stringify(data) }),
    update: (id: string, data: { status?: CaseStatus; priority?: CasePriority; assigned_to?: string; assigned_team?: string; due_at?: string; clear_due_at?: boolean; summary?: string; reason?: string; rationale?: string; confirm?: boolean; str_report_id?: string; filing_channel?: string; destination?: string; external_reference?: string; investigation_disposition?: string; str_candidate?: boolean; expected_updated_at?: string }) =>
      request<Case>(`/cases/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
    addNote: (id: string, author: string, content: string) =>
      request<CaseNote>(`/cases/${encodeURIComponent(id)}/notes`, {
        method: "POST",
        body: JSON.stringify({ author, content }),
      }),
    related: (id: string) => request<RelatedCase[]>(`/cases/${encodeURIComponent(id)}/related`),
    addRelated: (id: string, relatedCaseId: string, relationshipType?: string, rationale?: string, expectedUpdatedAt?: string) =>
      request<Case>(`/cases/${encodeURIComponent(id)}/related`, {
        method: "POST",
        body: JSON.stringify({ related_case_id: relatedCaseId, ...(relationshipType ? { relationship_type: relationshipType } : {}), ...(rationale ? { rationale } : {}), ...(expectedUpdatedAt ? { expected_updated_at: expectedUpdatedAt } : {}) }),
      }),
    removeRelated: (id: string, relationshipId: string, reason: string, expectedUpdatedAt?: string) => request<{ relationship_id: string; active: boolean }>(`/cases/${encodeURIComponent(id)}/related/${encodeURIComponent(relationshipId)}`, { method: "DELETE", body: JSON.stringify({ reason, ...(expectedUpdatedAt ? { expected_updated_at: expectedUpdatedAt } : {}) }) }),
    correctRelated: (id: string, relationshipId: string, relationshipType: string, rationale: string) => request<CaseRelationship>(`/cases/${encodeURIComponent(id)}/related/${encodeURIComponent(relationshipId)}`, { method: "PUT", body: JSON.stringify({ relationship_type: relationshipType, rationale }) }),
    file: (id: string) => request<CaseFile>(`/cases/${encodeURIComponent(id)}/timeline`),
    addEvidence: (id: string, data: { description: string; source: string; evidence_type: string; collected_at?: string; collected_by: string; integrity_hash?: string }) => request<CaseEvidence>(`/cases/${encodeURIComponent(id)}/evidence`, { method: "POST", body: JSON.stringify(data) }),
    correctEvidence: (id: string, evidenceID: string, data: { description?: string; source?: string; evidence_type?: string; collected_at?: string; collected_by?: string; integrity_hash?: string; reason: string }) => request<CaseEvidence>(`/cases/${encodeURIComponent(id)}/evidence/${encodeURIComponent(evidenceID)}/corrections`, { method: "POST", body: JSON.stringify(data) }),
    updateChecklist: (id: string, key: string, label: string, completed: boolean) => request<CaseChecklistItem>(`/cases/${encodeURIComponent(id)}/checklist/${encodeURIComponent(key)}`, { method: "PUT", body: JSON.stringify({ label, completed }) }),
    addWorkItem: (id: string, data: { title: string; description?: string; status?: string; assigned_to?: string; due_at?: string }) => request<CaseWorkItem>(`/cases/${encodeURIComponent(id)}/work-items`, { method: "POST", body: JSON.stringify(data) }),
    updateWorkItem: (id: string, itemId: string, data: { title: string; description?: string; status?: string; assigned_to?: string; due_at?: string }) => request<CaseWorkItem>(`/cases/${encodeURIComponent(id)}/work-items/${encodeURIComponent(itemId)}`, { method: "PATCH", body: JSON.stringify(data) }),
    downloadFile: async (id: string) => {
      const blob = await requestBlob(`/cases/${encodeURIComponent(id)}/export`)
      triggerDownload(blob, `case_${id}.json`)
    },
    exportFileUrl: (id: string) => `${BASE}/cases/${encodeURIComponent(id)}/export`,
  },
  operators: {
    directory: () => request<OperatorDirectory>("/operators"),
  },
  transactions: {
    // The API intentionally requires customer_id so an operator cannot
    // accidentally request an unbounded cross-customer transaction list.
    list: (customerId: string, params?: CursorPageParams) => {
      const qs = new URLSearchParams({ customer_id: customerId })
      if (params?.cursor) qs.set("cursor", params.cursor)
      if (params?.limit != null) qs.set("limit", String(params.limit))
      return request<PaginatedResponse<Transaction>>(`/transactions?${qs.toString()}`)
    },
    listAll: (customerId: string) =>
      listAllPages((page) => api.transactions.list(customerId, page)),
    get: (id: string) => request<Transaction>(`/transactions/${encodeURIComponent(id)}`),
    create: (data: { customer_id: string; external_id: string; amount: number; currency: string; direction: string; counterparty_id?: string; counterparty_country?: string; channel?: string; account_id?: string; counterparty?: Record<string, unknown>; metadata?: Record<string, unknown>; travel_rule_applicable?: boolean; travel_rule_evidence?: Record<string, unknown>; travel_rule_not_applicable_reason?: string; executed_at: string }, idempotencyKey?: string) =>
      request<Transaction>("/transactions", { method: "POST", body: JSON.stringify(data), ...(idempotencyKey ? { headers: { "Idempotency-Key": idempotencyKey } } : {}) }),
  },
  audit: {
    list: (params?: AuditListParams) => {
      const qs = buildAuditQuery(params)
      return request<PaginatedResponse<AuditEntry>>(`/audit${qs ? `?${qs}` : ""}`)
    },
    // export downloads the ALD-004 CSV/JSON export, preserving the same
    // filters as list(), by fetching the blob directly rather than a plain
    // <a href> (the export route requires auth.PermAuditRead, so it must go
    // through the same authenticated fetch() path as every other request).
    export: async (params?: AuditListParams, format: "csv" | "json" = "csv") => {
      const qs = new URLSearchParams(buildAuditQuery(params))
      qs.set("format", format)
      const blob = await requestBlob(`/audit/export?${qs.toString()}`)
      triggerDownload(blob, `audit_logs.${format}`)
    },
  },
  batch: {
    score: (customerIds?: string[]) =>
      request<BatchScoreResponse>("/batch/score", {
        method: "POST",
        body: JSON.stringify({ customer_ids: customerIds, target_mode: customerIds?.length ? "selected" : "all" }),
      }),
    monitor: (customerIds?: string[]) =>
      request<BatchMonitorResponse>("/batch/monitor", {
        method: "POST",
        body: JSON.stringify({ customer_ids: customerIds, target_mode: customerIds?.length ? "selected" : "all" }),
      }),
    preview: (data: { operation: string; target_mode: "selected" | "all"; customer_ids?: string[]; rationale?: string }) =>
      request<TargetManifest>("/batch/targets/preview", { method: "POST", body: JSON.stringify(data) }),
    confirm: (id: string, token: string, expectedVersion: number, rationale?: string) =>
      request<TargetManifest>(`/batch/targets/${encodeURIComponent(id)}/confirm`, { method: "POST", body: JSON.stringify({ token, expected_version: expectedVersion, rationale }) }),
    runs: (params?: CursorPageParams & { operation?: string; status?: string }) => {
      const qs = buildCursorQuery(params)
      if (params?.operation) qs.set("operation", params.operation)
      if (params?.status) qs.set("status", params.status)
      const query = qs.toString()
      return request<PaginatedResponse<BatchRun>>(`/batch/runs${query ? `?${query}` : ""}`)
    },
    createRun: (data: { operation: string; target_manifest_id: string; parameters?: Record<string, unknown>; rationale: string; idempotency_key?: string }) =>
      request<BatchRun>("/batch/runs", { method: "POST", body: JSON.stringify(data), ...(data.idempotency_key ? { headers: { "Idempotency-Key": data.idempotency_key } } : {}) }),
    getRun: (id: string) => request<BatchRun>(`/batch/runs/${encodeURIComponent(id)}`),
    rerun: (id: string) => request<BatchRun>(`/batch/runs/${encodeURIComponent(id)}/rerun`, { method: "POST" }),
  },
  pending: {
    list: (params?: CursorPageParams & { status?: string; customerId?: string; batchRunId?: string; createdFrom?: string; createdTo?: string; minAgeDays?: number; maxAgeDays?: number }) => {
      const qs = buildCursorQuery(params)
      if (params?.status) qs.set("status", params.status)
      if (params?.customerId) qs.set("customer_id", params.customerId)
      if (params?.batchRunId) qs.set("batch_run_id", params.batchRunId)
      if (params?.createdFrom) qs.set("created_from", params.createdFrom)
      if (params?.createdTo) qs.set("created_to", params.createdTo)
      if (params?.minAgeDays != null) qs.set("min_age_days", String(params.minAgeDays))
      if (params?.maxAgeDays != null) qs.set("max_age_days", String(params.maxAgeDays))
      const query = qs.toString()
      return request<PaginatedResponse<PendingEvaluation>>(`/pending-evaluations${query ? `?${query}` : ""}`)
    },
    get: (id: string) => request<PendingEvaluation>(`/pending-evaluations/${encodeURIComponent(id)}`),
    history: (id: string) => request<PendingEvaluationHistoryEntry[]>(`/pending-evaluations/${encodeURIComponent(id)}/history`),
    transition: (id: string, action: "retry" | "resolve" | "escalate", data: { reason: string; expected_version: number }) =>
      request<PendingEvaluation>(`/pending-evaluations/${encodeURIComponent(id)}/${action}`, { method: "POST", body: JSON.stringify(data) }),
  },
  reports: {
    list: (
      params?: {
        status?: "draft" | "submitted"
        customerId?: string
        alertId?: string
      } & CursorPageParams,
    ) => {
      const qs = new URLSearchParams()
      if (params?.status) qs.set("status", params.status)
      if (params?.customerId) qs.set("customer_id", params.customerId)
      if (params?.alertId) qs.set("alert_id", params.alertId)
      if (params?.cursor) qs.set("cursor", params.cursor)
      if (params?.limit != null) qs.set("limit", String(params.limit))
      const query = qs.toString()
      return request<PaginatedResponse<STRReport>>(`/reports/str${query ? `?${query}` : ""}`)
    },
    listAll: (params?: { status?: "draft" | "submitted"; customerId?: string; alertId?: string }) =>
      listAllPages((page) => api.reports.list({ ...params, ...page })),
    get: (id: string) => request<STRReport>(`/reports/str/${encodeURIComponent(id)}`),
    createSTR: (alertId: string, caseId: string, suspiciousPoint: string, createdBy: string) =>
      request<STRReport>("/reports/str", {
        method: "POST",
        body: JSON.stringify({ alert_id: alertId, case_id: caseId, suspicious_point: suspiciousPoint, created_by: createdBy }),
      }),
    update: (id: string, suspiciousPoint: string) =>
      request<STRReport>(`/reports/str/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: JSON.stringify({ suspicious_point: suspiciousPoint }),
      }),
    submit: (id: string, submissionEvidence: string, submittedBy?: string) =>
      request<STRReport>(`/reports/str/${encodeURIComponent(id)}/submit`, {
        method: "POST",
        body: JSON.stringify({
          submission_evidence: submissionEvidence,
          ...(submittedBy ? { submitted_by: submittedBy } : {}),
        }),
      }),
    // STR exports are keyed by the durable report ID. The alert-id form is
    // retained separately for older integrations during the contract
    // compatibility window.
    exportSTR: (reportId: string, format: "csv" | "json" = "csv") =>
      `${BASE}/reports/str/export?report_id=${encodeURIComponent(reportId)}&format=${format}`,
    downloadSTR: async (reportId: string, format: "csv" | "json" = "csv") => {
      const blob = await requestBlob(`/reports/str/export?report_id=${encodeURIComponent(reportId)}&format=${format}`)
      triggerDownload(blob, `str_${reportId}.${format}`)
    },
    exportSTRByAlert: (alertId: string, format: "csv" | "json" = "csv") =>
      `${BASE}/reports/str/export?alert_id=${encodeURIComponent(alertId)}&format=${format}`,
  },
  backtest: {
    create: async (input: { from: string; to: string; customer_ids?: string[]; scenario_ids?: string[]; baseline_rule_set_id: string; candidate_rule_set_id: string; rationale?: string; rerun_of?: string }, signal?: AbortSignal) =>
      normalizeBacktestJob(await request<BacktestJob>("/backtests", { method: "POST", body: JSON.stringify(input), signal })),
    get: async (id: string, signal?: AbortSignal) =>
      normalizeBacktestJob(await request<BacktestJob>(`/backtests/${encodeURIComponent(id)}`, { signal })),
    list: (params?: CursorPageParams) => {
      const qs = buildCursorQuery(params)
      const query = qs.toString()
      return request<PaginatedResponse<BacktestJob>>(`/backtests${query ? `?${query}` : ""}`).then((page) => ({ ...page, data: page.data.map(normalizeBacktestJob) }))
    },
    // Rule sets available for comparison, including inactive ones: a candidate
    // is compared before it is activated, so the generic rule listing's
    // active-only view could never offer it.
    discoverRules: (params?: CursorPageParams) => {
      const qs = buildCursorQuery(params)
      const query = qs.toString()
      return request<PaginatedResponse<RuleDefinition>>(`/backtests/rules${query ? `?${query}` : ""}`)
    },
    discoverAllRules: () => listAllPages((page) => api.backtest.discoverRules(page)),
    affectedCustomers: (id: string, params?: CursorPageParams & { scenarioId?: string }) => {
      const qs = buildCursorQuery(params)
      if (params?.scenarioId) qs.set("scenario_id", params.scenarioId)
      const query = qs.toString()
      return request<AffectedBacktestCustomersPage>(`/backtests/${encodeURIComponent(id)}/affected-customers${query ? `?${query}` : ""}`)
    },
    cancel: async (id: string) =>
      normalizeBacktestJob(await request<BacktestJob>(`/backtests/${encodeURIComponent(id)}/cancel`, { method: "POST" })),
    run: (customerIds: string[], scenarioIds: string[], description: string) =>
      request<BacktestResult>("/backtest", {
        method: "POST",
        body: JSON.stringify({ customer_ids: customerIds, scenario_ids: scenarioIds, description }),
      }).then(normalizeBacktestResult),
  },
  webhooks: {
    list: () => request<Webhook[]>("/webhooks"),
    get: (id: string) => request<Webhook>(`/webhooks/${encodeURIComponent(id)}`),
    create: (url: string, events: WebhookEventType[], secret?: string) =>
      request<Webhook>("/webhooks", {
        method: "POST",
        body: JSON.stringify({ url, events, secret }),
      }),
    delete: (id: string) =>
      request<{ status: string }>(`/webhooks/${encodeURIComponent(id)}`, { method: "DELETE" }),
    deliveries: (id: string) =>
      request<WebhookDelivery[]>(`/webhooks/${encodeURIComponent(id)}/deliveries`),
    listDLQ: () => request<WebhookDLQEntry[]>("/webhooks/dlq"),
    reprocessDLQ: (id: string) =>
      request<{ success: boolean; status_code: number }>(`/webhooks/dlq/${encodeURIComponent(id)}/reprocess`, {
        method: "POST",
      }),
  },
  admin: {
    apikeys: {
      list: () => request<APIKey[]>("/admin/apikeys"),
      create: (name: string, role: Role) =>
        request<APIKeyCreateResponse>("/admin/apikeys", {
          method: "POST",
          body: JSON.stringify({ name, role }),
        }),
      revoke: (id: string) =>
        request<{ status: string }>(`/admin/apikeys/${encodeURIComponent(id)}`, {
          method: "DELETE",
        }),
    },
  },
  config: {
    validate: (configType: string, yamlContent: string) =>
      request<ConfigValidationResult>("/config/validate", {
        method: "POST",
        body: JSON.stringify({ config_type: configType, yaml_content: yamlContent }),
      }),
  },
  rules: {
    list: (params?: { type?: RuleType; activeOnly?: boolean } & CursorPageParams) => {
      const qs = buildCursorQuery(params)
      if (params?.type) qs.set("type", params.type)
      if (params?.activeOnly) qs.set("is_active", "true")
      const q = qs.toString()
      return request<PaginatedResponse<RuleDefinition>>(`/rules${q ? `?${q}` : ""}`)
    },
    listAll: (params?: { type?: RuleType; activeOnly?: boolean }) =>
      listAllPages((page) => api.rules.list({ ...params, ...page })),
    get: (id: string, version?: number) =>
      request<RuleDefinition>(`/rules/${encodeURIComponent(id)}${version ? `?version=${version}` : ""}`),
    create: (data: { type: RuleType; name: string; description?: string; definition: unknown; is_active?: boolean }) =>
      request<RuleDefinition>("/rules", { method: "POST", body: JSON.stringify(data) }),
    update: (id: string, data: { description?: string; definition: unknown; is_active?: boolean }) =>
      request<RuleDefinition>(`/rules/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(data) }),
    activate: (id: string) =>
      request<RuleDefinition>(`/rules/${encodeURIComponent(id)}/activate`, { method: "POST" }),
    deactivate: (id: string) =>
      request<RuleDefinition>(`/rules/${encodeURIComponent(id)}/deactivate`, { method: "POST" }),
    import: (items: RuleImportItem[]) =>
      request<RuleDefinition[]>("/rules/import", { method: "POST", body: JSON.stringify(items) }),
    exportUrl: (id: string, format: "json" | "yaml" = "json") =>
      `${BASE}/rules/${encodeURIComponent(id)}/export?format=${format}`,
  },
  system: {
    info: () => request<SystemInfo>("/system/info"),
  },
  whitelist: {
    list: (status?: WhitelistEntryStatus) => {
      const qs = new URLSearchParams()
      if (status) qs.set("status", status)
      const q = qs.toString()
      return request<PaginatedResponse<WhitelistEntry>>(`/whitelist${q ? `?${q}` : ""}`)
    },
    get: (id: string) => request<WhitelistEntry>(`/whitelist/${encodeURIComponent(id)}`),
    create: (data: { customer_id: string; reason: string; valid_until: string; excluded_rule_ids?: string[] }) =>
      request<WhitelistEntry>("/whitelist", { method: "POST", body: JSON.stringify(data) }),
    approve: (id: string) =>
      request<WhitelistEntry>(`/whitelist/${encodeURIComponent(id)}/approve`, { method: "POST" }),
    revoke: (id: string) =>
      request<WhitelistEntry>(`/whitelist/${encodeURIComponent(id)}/revoke`, { method: "POST" }),
    review: (id: string, data: { decision: WhitelistReviewDecision; review_notes?: string; next_review_date?: string; new_valid_until?: string }) =>
      request<WhitelistReview>(`/whitelist/${encodeURIComponent(id)}/reviews`, {
        method: "POST",
        body: JSON.stringify(data),
      }),
  },
  auth: {
    login: (email: string, password: string) =>
      request<AuthUser>("/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      }),
    logout: () => request<{ status: string }>("/auth/logout", { method: "POST" }),
    refresh: () => request<{ status: string }>("/auth/refresh", { method: "POST" }),
    me: () => request<AuthUser>("/auth/me"),
  },
  setup: (email: string, password: string) =>
    request<AuthUser>("/setup", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  users: {
    list: () => request<User[]>("/admin/users"),
  },
}
