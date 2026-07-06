import { StatCard } from "@/components/stat-card"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
import { AlertTriangle, ArrowLeftRight, FolderOpen, ShieldAlert, ShieldCheck, Users } from "lucide-react"
import { useTranslation } from "react-i18next"
import {
  Bar,
  BarChart,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"

const RISK_COLORS: Record<string, string> = {
  low: "#22c55e",
  medium: "#eab308",
  high: "#ef4444",
  unscored: "#a3a3a3",
}

const SEVERITY_COLORS: Record<string, string> = {
  low: "#22c55e",
  medium: "#eab308",
  high: "#f97316",
  critical: "#ef4444",
}

const CASE_STATUS_LABEL_KEYS: Record<string, string> = {
  open: "open",
  investigating: "investigating",
  escalated: "escalated",
  closed: "closed",
  closed_true_positive: "closedTruePositive",
  closed_false_positive: "closedFalsePositive",
}

// WHITELIST_EXPIRING_SOON_DAYS mirrors the daily expiry job's notification
// window (api/internal/batch/whitelist_expiry.go, WL-006); it is not
// currently sourced from the API, so keep the two in sync manually if either
// changes.
const WHITELIST_EXPIRING_SOON_DAYS = 30

const LIST_TYPE_LABEL_KEYS: Record<string, string> = {
  sanctions: "sanctions",
  pep: "pep",
  "pep-rca": "pepRca",
}

export function DashboardPage() {
  const { t } = useTranslation()
  const { data: stats, loading, error } = useApi(api.dashboard)
  const { data: activeWhitelist } = useApi(() => api.whitelist.list("active"))

  const expiringSoonCount = (() => {
    if (!activeWhitelist || !Array.isArray(activeWhitelist.data)) return 0
    const threshold = Date.now() + WHITELIST_EXPIRING_SOON_DAYS * 24 * 60 * 60 * 1000
    return activeWhitelist.data.filter((e) => new Date(e.valid_until).getTime() < threshold).length
  })()

  if (loading) {
    return <DashboardSkeleton />
  }

  if (error) {
    return (
      <div className="flex items-center justify-center p-12">
        <p className="text-destructive">{t("dashboard.errorPrefix", { error })}</p>
      </div>
    )
  }

  if (!stats) return null

  const riskData = Object.entries(stats.customers_by_risk_tier).map(([key, value]) => ({
    name: t(`dashboard.risk.${key}`, { defaultValue: key }),
    value,
    color: RISK_COLORS[key] ?? "#a3a3a3",
  }))

  const severityData = Object.entries(stats.alerts_by_severity).map(([key, value]) => ({
    name: t(`dashboard.severity.${key}`, { defaultValue: key }),
    value,
    fill: SEVERITY_COLORS[key] ?? "#a3a3a3",
  }))

  const caseStatusData = Object.entries(stats.cases_by_status).map(([key, value]) => ({
    name: t(`dashboard.caseStatusLabel.${CASE_STATUS_LABEL_KEYS[key] ?? key}`, { defaultValue: key }),
    value,
  }))

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("dashboard.title")}</h1>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatCard title={t("dashboard.stats.customers")} value={stats.total_customers} icon={Users} />
        <StatCard
          title={t("dashboard.stats.alerts")}
          value={stats.total_alerts}
          icon={AlertTriangle}
          description={t("dashboard.stats.alertsDescription")}
        />
        <StatCard
          title={t("dashboard.stats.cases")}
          value={stats.total_cases}
          icon={FolderOpen}
          description={t("dashboard.stats.casesDescription")}
        />
        <StatCard
          title={t("dashboard.stats.recentTransactions")}
          value={stats.recent_transactions}
          icon={ArrowLeftRight}
        />
        <StatCard
          title={t("dashboard.stats.whitelistExpiringSoon")}
          value={expiringSoonCount}
          icon={ShieldCheck}
          description={t("dashboard.stats.whitelistExpiringSoonDescription", { days: WHITELIST_EXPIRING_SOON_DAYS })}
        />
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.charts.riskDistribution")}</CardTitle>
          </CardHeader>
          <CardContent>
            {riskData.length > 0 ? (
              <ResponsiveContainer width="100%" height={240}>
                <PieChart>
                  <Pie
                    data={riskData}
                    cx="50%"
                    cy="50%"
                    innerRadius={60}
                    outerRadius={90}
                    dataKey="value"
                    nameKey="name"
                    label={({ name, value }) => `${name}: ${value}`}
                  >
                    {riskData.map((entry) => (
                      <Cell key={entry.name} fill={entry.color} />
                    ))}
                  </Pie>
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <p className="py-12 text-center text-sm text-muted-foreground">{t("dashboard.noData")}</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.charts.severityDistribution")}</CardTitle>
          </CardHeader>
          <CardContent>
            {severityData.length > 0 ? (
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={severityData}>
                  <XAxis dataKey="name" />
                  <YAxis allowDecimals={false} />
                  <Tooltip />
                  <Bar dataKey="value" radius={[4, 4, 0, 0]}>
                    {severityData.map((entry) => (
                      <Cell key={entry.name} fill={entry.fill} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <p className="py-12 text-center text-sm text-muted-foreground">{t("dashboard.noData")}</p>
            )}
          </CardContent>
        </Card>
      </div>

      {stats.screening_list_freshness && stats.screening_list_freshness.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldAlert className="h-4 w-4" />
              {t("dashboard.charts.screeningFreshness")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-4">
              {stats.screening_list_freshness.map((list) => (
                <div key={list.list_id} className="flex items-center gap-2">
                  <Badge variant="outline">
                    {t(`dashboard.listType.${LIST_TYPE_LABEL_KEYS[list.list_type] ?? list.list_type}`, {
                      defaultValue: list.list_type,
                    })}
                  </Badge>
                  <span className="text-sm text-muted-foreground">{list.list_id}</span>
                  <Badge variant={list.needs_operational_alert ? "destructive" : "secondary"}>
                    {list.stale_days === 0
                      ? t("dashboard.freshness.upToDate")
                      : t("dashboard.freshness.staleDays", { days: list.stale_days })}
                  </Badge>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {caseStatusData.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.charts.caseStatusTitle")}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex gap-4">
              {caseStatusData.map(({ name, value }) => (
                <div key={name} className="flex items-center gap-2">
                  <Badge variant="secondary">{name}</Badge>
                  <span className="text-sm font-medium">{value}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function DashboardSkeleton() {
  return (
    <div className="space-y-6">
      <div className="h-8 w-48 animate-pulse rounded bg-muted" />
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="h-28 animate-pulse rounded-xl border bg-muted" />
        ))}
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        {Array.from({ length: 2 }).map((_, i) => (
          <div key={i} className="h-72 animate-pulse rounded-xl border bg-muted" />
        ))}
      </div>
    </div>
  )
}
