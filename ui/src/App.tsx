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
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App
