const BASE = "/api/v1"

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`API ${res.status}: ${body}`)
  }
  return res.json()
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
}

export type RiskTier = "low" | "medium" | "high"
export type AlertSeverity = "low" | "medium" | "high" | "critical"
export type AlertStatus = "open" | "investigating" | "escalated" | "closed_true_positive" | "closed_false_positive"
export type CaseStatus = "open" | "investigating" | "escalated" | "closed"
export type CasePriority = "low" | "medium" | "high"

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

export type WebhookEventType =
  | "alert.created" | "alert.resolved"
  | "case.created" | "case.updated" | "case.closed"
  | "str.created" | "score.changed" | "screening.match"

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
  },
  cases: {
    list: () => request<Case[]>("/cases"),
    get: (id: string) => request<Case>(`/cases/${encodeURIComponent(id)}`),
    create: (data: { customer_id: string; alert_ids: string[]; priority: string; assigned_to?: string; summary: string }) =>
      request<Case>("/cases", { method: "POST", body: JSON.stringify(data) }),
    update: (id: string, data: { status?: CaseStatus; assigned_to?: string; summary?: string }) =>
      request<Case>(`/cases/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
    addNote: (id: string, author: string, content: string) =>
      request<CaseNote>(`/cases/${encodeURIComponent(id)}/notes`, {
        method: "POST",
        body: JSON.stringify({ author, content }),
      }),
  },
  transactions: {
    list: () => request<Transaction[]>("/transactions"),
    get: (id: string) => request<Transaction>(`/transactions/${encodeURIComponent(id)}`),
    create: (data: { customer_id: string; external_id: string; amount: number; currency: string; direction: string; counterparty_id?: string; counterparty_country?: string; channel?: string; executed_at: string }) =>
      request<Transaction>("/transactions", { method: "POST", body: JSON.stringify(data) }),
  },
  audit: {
    list: (resourceType?: string, resourceId?: string) => {
      const params = new URLSearchParams()
      if (resourceType) params.set("resource_type", resourceType)
      if (resourceId) params.set("resource_id", resourceId)
      const qs = params.toString()
      return request<AuditEntry[]>(`/audit${qs ? `?${qs}` : ""}`)
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
  system: {
    info: () => request<SystemInfo>("/system/info"),
  },
}
