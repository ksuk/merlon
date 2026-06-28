import { cn } from "@/lib/utils"
import {
  LayoutDashboard,
  Users,
  AlertTriangle,
  FolderOpen,
  ArrowLeftRight,
  Shield,
  FileText,
  FlaskConical,
  ScrollText,
  Settings,
  Webhook,
} from "lucide-react"
import { Link, useLocation } from "react-router-dom"

const navItems = [
  { to: "/", label: "ダッシュボード", icon: LayoutDashboard },
  { to: "/customers", label: "顧客", icon: Users },
  { to: "/alerts", label: "アラート", icon: AlertTriangle },
  { to: "/cases", label: "ケース", icon: FolderOpen },
  { to: "/transactions", label: "取引", icon: ArrowLeftRight },
  { to: "/reports", label: "STRレポート", icon: FileText },
  { to: "/backtest", label: "バックテスト", icon: FlaskConical },
  { to: "/webhooks", label: "Webhook", icon: Webhook },
  { to: "/audit", label: "監査ログ", icon: ScrollText },
  { to: "/system", label: "システム", icon: Settings },
]

export function Sidebar() {
  const location = useLocation()

  return (
    <aside className="flex h-screen w-60 flex-col border-r bg-sidebar">
      <div className="flex h-14 items-center gap-2 border-b px-4">
        <Shield className="h-6 w-6 text-primary" />
        <span className="text-lg font-bold tracking-tight">Merlon</span>
      </div>
      <nav className="flex-1 space-y-1 p-2">
        {navItems.map((item) => {
          const active =
            item.to === "/"
              ? location.pathname === "/"
              : location.pathname.startsWith(item.to)
          return (
            <Link
              key={item.to}
              to={item.to}
              className={cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                active
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
              )}
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </Link>
          )
        })}
      </nav>
      <div className="border-t p-4 text-xs text-muted-foreground">
        AML/CFT Compliance Platform
      </div>
    </aside>
  )
}
