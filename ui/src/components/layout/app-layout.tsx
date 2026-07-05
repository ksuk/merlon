import { Suspense } from "react"
import { Outlet } from "react-router-dom"
import { LanguageSwitcher } from "./language-switcher"
import { Sidebar } from "./sidebar"

function PageLoader() {
  return (
    <div className="flex min-h-[200px] items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-muted border-t-primary" />
    </div>
  )
}

export function AppLayout() {
  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-14 shrink-0 items-center justify-end border-b px-6">
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
