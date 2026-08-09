import { LogOut } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useSession } from "@/hooks/use-session"

/**
 * SessionMenu identifies the active session in the shell and offers a
 * deliberate way to end it.
 *
 * AML decisions and handoffs depend on knowing whose session produced them.
 * The shell previously showed neither the operator nor the authentication
 * mode, so an evaluation deployment with authentication switched off looked
 * exactly like a production login (#81).
 */
export function SessionMenu() {
  const { t } = useTranslation()
  const { user, userState, authMode, logout, loggingOut, logoutError } = useSession()

  // The mode is only worth stating when it changes what the operator should
  // conclude. A normal session needs no label; the other two do.
  const modeLabel =
    authMode === "disabled"
      ? t("layout.session.authMode.disabled")
      : authMode === "api_key_only"
        ? t("layout.session.authMode.apiKeyOnly")
        : null

  return (
    <div className="flex items-center gap-3">
      {modeLabel ? (
        <Badge
          variant="outline"
          className="border-amber-500/40 bg-amber-500/10 text-amber-700"
          title={t("layout.session.authMode.hint")}
        >
          {modeLabel}
        </Badge>
      ) : null}

      <div className="flex flex-col items-end leading-tight">
        {userState === "identified" && user ? (
          <>
            <span className="text-sm font-medium">{user.email}</span>
            <span className="text-xs text-muted-foreground">
              {t(`layout.session.role.${user.role}`, { defaultValue: user.role })}
            </span>
          </>
        ) : (
          <span className="text-xs text-muted-foreground" data-testid="session-user-state">
            {t(`layout.session.userState.${userState}`)}
          </span>
        )}
      </div>

      {userState === "identified" ? (
        <Button
          variant="outline"
          size="sm"
          onClick={() => void logout()}
          disabled={loggingOut}
          aria-busy={loggingOut}
        >
          <LogOut className="mr-2 h-4 w-4" aria-hidden="true" />
          {loggingOut ? t("layout.session.loggingOut") : t("layout.session.logout")}
        </Button>
      ) : null}

      {logoutError ? (
        <span role="alert" className="text-xs text-destructive">
          {t("layout.session.logoutFailed")}
        </span>
      ) : null}
    </div>
  )
}
