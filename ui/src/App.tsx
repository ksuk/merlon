import { BrowserRouter, Route, Routes } from "react-router-dom"
import { AppLayout } from "@/components/layout/app-layout"
import { DashboardPage } from "@/pages/dashboard"
import { CustomersPage } from "@/pages/customers"
import { AlertsPage } from "@/pages/alerts"
import { CasesPage } from "@/pages/cases"
import { TransactionsPage } from "@/pages/transactions"

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="customers" element={<CustomersPage />} />
          <Route path="alerts" element={<AlertsPage />} />
          <Route path="cases" element={<CasesPage />} />
          <Route path="transactions" element={<TransactionsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App
