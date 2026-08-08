import { Suspense } from "react"
import { useTranslation } from "react-i18next"
import { Outlet } from "react-router"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
import { LanguageSwitcher } from "./language-switcher"
import { SessionMenu } from "./session-menu"
import { Sidebar } from "./sidebar"

function PageLoader() {
  return (
    <div className="flex min-h-[200px] items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-muted border-t-primary" />
    </div>
  )
}

// DemoDataBadge is a small, display-only indicator (PH7 DD3) that a
// recording/screenshot is running against synthetic demo data. It carries no
// functional gating — it only renders when the API reports
// features.demo_data === true (GET /api/v1/system/info).
function DemoDataBadge() {
  const { t } = useTranslation()
  const { data } = useApi(api.system.info)

  if (!data?.features?.demo_data) {
    return null
  }

  return (
    <span className="rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-xs text-amber-700">
      {t("layout.demoDataBadge")}
    </span>
  )
}

export function AppLayout() {
  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-14 shrink-0 items-center justify-end gap-3 border-b px-6">
          <DemoDataBadge />
          <SessionMenu />
          <LanguageSwitcher />
        </header>
        <main className="flex-1 overflow-y-auto bg-background p-6">
          <Suspense fallback={<PageLoader />}>
            <Outlet />
          </Suspense>
        </main>
      </div>
    </div>
  )
}
