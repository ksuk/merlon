import { cn } from "@/lib/utils"
import { BrandLogo } from "@/components/brand-logo"
import {
  LayoutDashboard,
  Users,
  AlertTriangle,
  FolderOpen,
  ArrowLeftRight,
  FileText,
  FlaskConical,
  KeyRound,
  Layers,
  ScrollText,
  Settings,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Webhook,
  ShieldAlert,
  RotateCcw,
  UserCog,
  ClipboardCheck,
} from "lucide-react"
import { useTranslation } from "react-i18next"
import { Link, useLocation } from "react-router"
import { useSession } from "@/hooks/use-session"

// capability, when set, is the server-reported capability that decides whether
// this entry is offered, disabled with a reason, or hidden. Entries without one
// are reachable in every deployment.
//
// Hiding an entry is not authorization: the route behind it enforces the same
// permission server-side. The point is that an operator learns why a function
// is not available instead of finding a menu item that always fails.
interface NavItem {
  to: string
  labelKey: string
  icon: typeof LayoutDashboard
  capability?: string
}

const navItems: NavItem[] = [
  { to: "/", labelKey: "nav.dashboard", icon: LayoutDashboard },
  { to: "/customers", labelKey: "nav.customers", icon: Users },
  { to: "/customer-reviews", labelKey: "nav.customerReviews", icon: ClipboardCheck },
  { to: "/alerts", labelKey: "nav.alerts", icon: AlertTriangle },
  { to: "/cases", labelKey: "nav.cases", icon: FolderOpen },
  { to: "/transactions", labelKey: "nav.transactions", icon: ArrowLeftRight },
  { to: "/batch", labelKey: "nav.batch", icon: Layers },
  { to: "/screening-queue", labelKey: "nav.screeningQueue", icon: ShieldAlert },
  { to: "/pending-evaluations", labelKey: "nav.pendingEvaluations", icon: RotateCcw },
  { to: "/reports", labelKey: "nav.reports", icon: FileText },
  { to: "/backtest", labelKey: "nav.backtest", icon: FlaskConical },
  { to: "/webhooks", labelKey: "nav.webhooks", icon: Webhook, capability: "webhooks.manage" },
  { to: "/apikeys", labelKey: "nav.apikeys", icon: KeyRound, capability: "api_keys.manage" },
  { to: "/users", labelKey: "nav.users", icon: UserCog, capability: "users.manage" },
  { to: "/rules", labelKey: "nav.rules", icon: SlidersHorizontal },
  { to: "/whitelist", labelKey: "nav.whitelist", icon: ShieldCheck },
  { to: "/config", labelKey: "nav.config", icon: Settings2, capability: "config.validate" },
  { to: "/audit", labelKey: "nav.audit", icon: ScrollText },
  { to: "/system", labelKey: "nav.system", icon: Settings },
]

export function Sidebar() {
  const location = useLocation()
  const { t } = useTranslation()
  const { capabilityFor, capabilities } = useSession()

  // Until the contract has been read, every entry stays visible. Hiding
  // navigation during a request would make the shell flicker functions in and
  // out, and an operator cannot tell a slow response from a removed feature.
  const visible = navItems.filter((item) => {
    if (!item.capability || !capabilities) return true
    const descriptor = capabilityFor(item.capability)
    return descriptor === null || descriptor.availability !== "not_configured"
  })

  return (
    <aside className="flex h-screen w-60 flex-col border-r bg-sidebar">
      <div className="flex h-14 items-center border-b px-4">
        <BrandLogo className="h-8" />
      </div>
      <nav className="flex-1 space-y-1 p-2">
        {visible.map((item) => {
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
              {t(item.labelKey)}
            </Link>
          )
        })}
      </nav>
      <div className="border-t p-4 text-xs text-muted-foreground">{t("nav.tagline")}</div>
    </aside>
  )
}
