import { BrowserRouter, Route, Routes } from "react-router-dom"
import { AppLayout } from "@/components/layout/app-layout"
import { DashboardPage } from "@/pages/dashboard"
import { CustomersPage } from "@/pages/customers"
import { CustomerDetailPage } from "@/pages/customer-detail"
import { AlertsPage } from "@/pages/alerts"
import { AlertDetailPage } from "@/pages/alert-detail"
import { CasesPage } from "@/pages/cases"
import { CaseDetailPage } from "@/pages/case-detail"
import { TransactionsPage } from "@/pages/transactions"
import { APIKeysPage } from "@/pages/apikeys"
import { ConfigPage } from "@/pages/config"
import { ReportsPage } from "@/pages/reports"
import { BacktestPage } from "@/pages/backtest"
import { WebhooksPage } from "@/pages/webhooks"
import { AuditPage } from "@/pages/audit"
import { SystemPage } from "@/pages/system"

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="customers" element={<CustomersPage />} />
          <Route path="customers/:id" element={<CustomerDetailPage />} />
          <Route path="alerts" element={<AlertsPage />} />
          <Route path="alerts/:id" element={<AlertDetailPage />} />
          <Route path="cases" element={<CasesPage />} />
          <Route path="cases/:id" element={<CaseDetailPage />} />
          <Route path="transactions" element={<TransactionsPage />} />
          <Route path="reports" element={<ReportsPage />} />
          <Route path="backtest" element={<BacktestPage />} />
          <Route path="webhooks" element={<WebhooksPage />} />
          <Route path="apikeys" element={<APIKeysPage />} />
          <Route path="config" element={<ConfigPage />} />
          <Route path="audit" element={<AuditPage />} />
          <Route path="system" element={<SystemPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App
