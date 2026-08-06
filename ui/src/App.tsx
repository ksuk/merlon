import { lazy } from "react";
import { BrowserRouter, Route, Routes } from "react-router";
import { AuthGate } from "@/components/auth-gate";
import { AppLayout } from "@/components/layout/app-layout";
import { ErrorBoundary } from "@/components/error-boundary";

const DashboardPage = lazy(() =>
  import("@/pages/dashboard").then((m) => ({ default: m.DashboardPage })),
);
const CustomersPage = lazy(() =>
  import("@/pages/customers").then((m) => ({ default: m.CustomersPage })),
);
const CustomerDetailPage = lazy(() =>
  import("@/pages/customer-detail").then((m) => ({
    default: m.CustomerDetailPage,
  })),
);
const AlertsPage = lazy(() =>
  import("@/pages/alerts").then((m) => ({ default: m.AlertsPage })),
);
const AlertDetailPage = lazy(() =>
  import("@/pages/alert-detail").then((m) => ({ default: m.AlertDetailPage })),
);
const CasesPage = lazy(() =>
  import("@/pages/cases").then((m) => ({ default: m.CasesPage })),
);
const CaseDetailPage = lazy(() =>
  import("@/pages/case-detail").then((m) => ({ default: m.CaseDetailPage })),
);
const TransactionsPage = lazy(() =>
  import("@/pages/transactions").then((m) => ({ default: m.TransactionsPage })),
);
const TransactionDetailPage = lazy(() =>
  import("@/pages/transaction-detail").then((m) => ({
    default: m.TransactionDetailPage,
  })),
);
const BatchPage = lazy(() =>
  import("@/pages/batch").then((m) => ({ default: m.BatchPage })),
);
const ScreeningQueuePage = lazy(() =>
  import("@/pages/screening-queue").then((m) => ({ default: m.ScreeningQueuePage })),
);
const PendingEvaluationsPage = lazy(() =>
  import("@/pages/pending-evaluations").then((m) => ({ default: m.PendingEvaluationsPage })),
);
const ReportsPage = lazy(() =>
  import("@/pages/reports").then((m) => ({ default: m.ReportsPage })),
);
const BacktestPage = lazy(() =>
  import("@/pages/backtest").then((m) => ({ default: m.BacktestPage })),
);
const WebhooksPage = lazy(() =>
  import("@/pages/webhooks").then((m) => ({ default: m.WebhooksPage })),
);
const APIKeysPage = lazy(() =>
  import("@/pages/apikeys").then((m) => ({ default: m.APIKeysPage })),
);
const ConfigPage = lazy(() =>
  import("@/pages/config").then((m) => ({ default: m.ConfigPage })),
);
const RulesPage = lazy(() =>
  import("@/pages/rules").then((m) => ({ default: m.RulesPage })),
);
const WhitelistPage = lazy(() =>
  import("@/pages/whitelist").then((m) => ({ default: m.WhitelistPage })),
);
const AuditPage = lazy(() =>
  import("@/pages/audit").then((m) => ({ default: m.AuditPage })),
);
const SystemPage = lazy(() =>
  import("@/pages/system").then((m) => ({ default: m.SystemPage })),
);
const UsersPage = lazy(() =>
  import("@/pages/users").then((m) => ({ default: m.UsersPage })),
);
const LoginPage = lazy(() =>
  import("@/pages/login").then((m) => ({ default: m.LoginPage })),
);
const SetupPage = lazy(() =>
  import("@/pages/setup").then((m) => ({ default: m.SetupPage })),
);
const NotFoundPage = lazy(() =>
  import("@/pages/not-found").then((m) => ({ default: m.NotFoundPage })),
);

function App() {
  return (
    <BrowserRouter>
      <ErrorBoundary>
        <Routes>
          <Route path="login" element={<LoginPage />} />
          <Route path="setup" element={<SetupPage />} />
          <Route element={<AuthGate />}>
            <Route element={<AppLayout />}>
              <Route index element={<DashboardPage />} />
              <Route path="customers" element={<CustomersPage />} />
              <Route path="customers/:id" element={<CustomerDetailPage />} />
              <Route path="alerts" element={<AlertsPage />} />
              <Route path="alerts/:id" element={<AlertDetailPage />} />
              <Route path="cases" element={<CasesPage />} />
              <Route path="cases/:id" element={<CaseDetailPage />} />
              <Route path="transactions" element={<TransactionsPage />} />
              <Route
                path="transactions/:id"
                element={<TransactionDetailPage />}
              />
              <Route path="batch" element={<BatchPage />} />
              <Route path="screening-queue" element={<ScreeningQueuePage />} />
              <Route path="pending-evaluations" element={<PendingEvaluationsPage />} />
              <Route path="reports" element={<ReportsPage />} />
              <Route path="backtest" element={<BacktestPage />} />
              <Route path="webhooks" element={<WebhooksPage />} />
              <Route path="apikeys" element={<APIKeysPage />} />
              <Route path="users" element={<UsersPage />} />
              <Route path="config" element={<ConfigPage />} />
              <Route path="rules" element={<RulesPage />} />
              <Route path="whitelist" element={<WhitelistPage />} />
              <Route path="audit" element={<AuditPage />} />
              <Route path="system" element={<SystemPage />} />
              <Route path="*" element={<NotFoundPage />} />
            </Route>
          </Route>
        </Routes>
      </ErrorBoundary>
    </BrowserRouter>
  );
}

export default App;
