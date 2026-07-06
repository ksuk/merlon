const BASE = "/api/v1"

// getCookie reads the non-HttpOnly csrf_token cookie set at login, so it can
// be echoed back per the Double Submit Cookie CSRF scheme (auth.md §2).
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

export interface ScreeningListFreshnessStat {
  list_id: string
  list_type: string
  stale_days: number
  needs_operational_alert: boolean
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
  screening_list_freshness?: ScreeningListFreshnessStat[]
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
  attributes: Record<string, string>
  risk_score?: number
  risk_tier?: RiskTier
  last_scored_at?: string
  created_at: string
  updated_at: string
  // EDD 3段階エスカレーション（case-management.md §EDD未実施継続時の段階的
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
}

export interface Case {
  id: string
  customer_id: string
  alert_ids: string[]
  status: CaseStatus
  priority: CasePriority
  assigned_to?: string
  summary: string
  notes?: CaseNote[]
  reopen_reason?: string
  related_case_ids?: string[]
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

// RelatedCase pairs a case with how it was linked (case-management.md
// §ケース間の関連付け: 同一顧客の自動抽出 or 手動リンク).
export interface RelatedCase {
  case: Case
  link_type: "auto" | "manual"
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
  scored_at: string
}

export interface Factor {
  name: string
  axis: string
  score: number
  description: string
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
  report_type: string
  status: string
  suspicious_point: string
  transaction_ids: string[]
  total_amount: number
  currency: string
  created_at: string
  submitted_at?: string
  created_by: string
}

export interface BacktestResult {
  backtest_id: string
  total_transactions: number
  total_customers: number
  total_alerts: number
  scenario_results: BacktestScenarioResult[]
  execution_time_ms: number
}

export interface BacktestScenarioResult {
  scenario_id: string
  alerts_generated: number
  high_severity_count: number
  medium_severity_count: number
  low_severity_count: number
  affected_customer_ids: string[]
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
  error?: string
}

export interface BatchMonitorResponse {
  total: number
  succeeded: number
  failed: number
  alerts_total: number
  results: BatchMonitorResult[]
  duration: string
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

export const api = {
  dashboard: () => request<DashboardStats>("/dashboard"),
  customers: {
    list: () => request<Customer[]>("/customers"),
    get: (id: string) => request<Customer>(`/customers/${encodeURIComponent(id)}`),
    create: (data: { external_id: string; customer_type: string; country_code: string; product_types: string[]; attributes: Record<string, string> }) =>
      request<Customer>("/customers", { method: "POST", body: JSON.stringify(data) }),
    update: (id: string, data: { country_code?: string; product_types?: string[]; attributes?: Record<string, string> }) =>
      request<Customer>(`/customers/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(data) }),
    scoreHistory: (id: string) =>
      request<ScoreRecord[]>(`/customers/${encodeURIComponent(id)}/scores`),
    score: (id: string, ruleSetId: string) =>
      request<ScoreRecord>(`/customers/${encodeURIComponent(id)}/score`, {
        method: "POST",
        body: JSON.stringify({ rule_set_id: ruleSetId }),
      }),
    screen: (id: string, listIds: string[]) =>
      request<ScreenResult>(`/customers/${encodeURIComponent(id)}/screen`, {
        method: "POST",
        body: JSON.stringify({ list_ids: listIds }),
      }),
  },
  alerts: {
    list: () => request<Alert[]>("/alerts"),
    get: (id: string) => request<Alert>(`/alerts/${encodeURIComponent(id)}`),
    updateStatus: (id: string, status: AlertStatus) =>
      request<Alert>(`/alerts/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify({ status }),
      }),
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
    list: () => request<Case[]>("/cases"),
    get: (id: string) => request<Case>(`/cases/${encodeURIComponent(id)}`),
    create: (data: { customer_id: string; alert_ids: string[]; priority: string; assigned_to?: string; summary: string }) =>
      request<Case>("/cases", { method: "POST", body: JSON.stringify(data) }),
    update: (id: string, data: { status?: CaseStatus; assigned_to?: string; summary?: string; reason?: string }) =>
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
    addRelated: (id: string, relatedCaseId: string) =>
      request<Case>(`/cases/${encodeURIComponent(id)}/related`, {
        method: "POST",
        body: JSON.stringify({ related_case_id: relatedCaseId }),
      }),
  },
  transactions: {
    list: () => request<Transaction[]>("/transactions"),
    get: (id: string) => request<Transaction>(`/transactions/${encodeURIComponent(id)}`),
    create: (data: { customer_id: string; external_id: string; amount: number; currency: string; direction: string; counterparty_id?: string; counterparty_country?: string; channel?: string; executed_at: string }) =>
      request<Transaction>("/transactions", { method: "POST", body: JSON.stringify(data) }),
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
      const res = await fetch(`${BASE}/audit/export?${qs.toString()}`)
      if (!res.ok) {
        const body = await res.text()
        throw new Error(`API ${res.status}: ${body}`)
      }
      const blob = await res.blob()
      const link = document.createElement("a")
      link.href = URL.createObjectURL(blob)
      link.download = `audit_logs.${format}`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(link.href)
    },
  },
  batch: {
    score: (customerIds?: string[]) =>
      request<BatchScoreResponse>("/batch/score", {
        method: "POST",
        body: JSON.stringify({ customer_ids: customerIds }),
      }),
    monitor: (customerIds?: string[]) =>
      request<BatchMonitorResponse>("/batch/monitor", {
        method: "POST",
        body: JSON.stringify({ customer_ids: customerIds }),
      }),
  },
  reports: {
    createSTR: (alertId: string, suspiciousPoint: string, createdBy: string) =>
      request<STRReport>("/reports/str", {
        method: "POST",
        body: JSON.stringify({ alert_id: alertId, suspicious_point: suspiciousPoint, created_by: createdBy }),
      }),
    exportSTR: (alertId: string, format: "csv" | "json" = "csv") =>
      `${BASE}/reports/str/export?alert_id=${encodeURIComponent(alertId)}&format=${format}`,
  },
  backtest: {
    run: (customerIds: string[], scenarioIds: string[], description: string) =>
      request<BacktestResult>("/backtest", {
        method: "POST",
        body: JSON.stringify({ customer_ids: customerIds, scenario_ids: scenarioIds, description }),
      }),
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
    list: (params?: { type?: RuleType; activeOnly?: boolean }) => {
      const qs = new URLSearchParams()
      if (params?.type) qs.set("type", params.type)
      if (params?.activeOnly) qs.set("is_active", "true")
      const q = qs.toString()
      return request<PaginatedResponse<RuleDefinition>>(`/rules${q ? `?${q}` : ""}`)
    },
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
