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
    scoreHistory: (id: string) =>
      request<ScoreRecord[]>(`/customers/${encodeURIComponent(id)}/scores`),
    score: (id: string, ruleSetId: string) =>
      request<ScoreRecord>(`/customers/${encodeURIComponent(id)}/score`, {
        method: "POST",
        body: JSON.stringify({ rule_set_id: ruleSetId }),
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
  system: {
    info: () => request<SystemInfo>("/system/info"),
  },
}
