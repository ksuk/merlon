import { StatCard } from "@/components/stat-card"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
import { AlertTriangle, ArrowLeftRight, FolderOpen, ShieldAlert, Users } from "lucide-react"
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

const RISK_LABELS: Record<string, string> = {
  low: "低リスク",
  medium: "中リスク",
  high: "高リスク",
  unscored: "未スコア",
}

const SEVERITY_LABELS: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
  critical: "重大",
}

const LIST_TYPE_LABELS: Record<string, string> = {
  sanctions: "制裁リスト",
  pep: "PEPリスト",
  "pep-rca": "PEP家族・近親者リスト",
}

const STATUS_LABELS: Record<string, string> = {
  open: "未対応",
  investigating: "調査中",
  escalated: "エスカレーション",
  closed: "完了",
  closed_true_positive: "完了(真陽性)",
  closed_false_positive: "完了(偽陽性)",
}

export function DashboardPage() {
  const { data: stats, loading, error } = useApi(api.dashboard)

  if (loading) {
    return <DashboardSkeleton />
  }

  if (error) {
    return (
      <div className="flex items-center justify-center p-12">
        <p className="text-destructive">データの取得に失敗しました: {error}</p>
      </div>
    )
  }

  if (!stats) return null

  const riskData = Object.entries(stats.customers_by_risk_tier).map(([key, value]) => ({
    name: RISK_LABELS[key] ?? key,
    value,
    color: RISK_COLORS[key] ?? "#a3a3a3",
  }))

  const severityData = Object.entries(stats.alerts_by_severity).map(([key, value]) => ({
    name: SEVERITY_LABELS[key] ?? key,
    value,
    fill: SEVERITY_COLORS[key] ?? "#a3a3a3",
  }))

  const caseStatusData = Object.entries(stats.cases_by_status).map(([key, value]) => ({
    name: STATUS_LABELS[key] ?? key,
    value,
  }))

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">ダッシュボード</h1>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatCard title="顧客数" value={stats.total_customers} icon={Users} />
        <StatCard title="アラート" value={stats.total_alerts} icon={AlertTriangle} description="未解決" />
        <StatCard title="ケース" value={stats.total_cases} icon={FolderOpen} description="オープン" />
        <StatCard
          title="直近の取引"
          value={stats.recent_transactions}
          icon={ArrowLeftRight}
        />
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">顧客リスク分布</CardTitle>
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
              <p className="py-12 text-center text-sm text-muted-foreground">データなし</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">アラート深刻度</CardTitle>
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
              <p className="py-12 text-center text-sm text-muted-foreground">データなし</p>
            )}
          </CardContent>
        </Card>
      </div>

      {stats.screening_list_freshness && stats.screening_list_freshness.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldAlert className="h-4 w-4" />
              制裁・PEPリストの鮮度
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-4">
              {stats.screening_list_freshness.map((list) => (
                <div key={list.list_id} className="flex items-center gap-2">
                  <Badge variant="outline">{LIST_TYPE_LABELS[list.list_type] ?? list.list_type}</Badge>
                  <span className="text-sm text-muted-foreground">{list.list_id}</span>
                  <Badge variant={list.needs_operational_alert ? "destructive" : "secondary"}>
                    {list.stale_days === 0 ? "最新" : `${list.stale_days}日経過`}
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
            <CardTitle className="text-base">ケースステータス</CardTitle>
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
