import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, Outlet, useLocation } from "react-router";
import { ApiError, api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { SessionProvider } from "@/components/session-provider";

type AuthState = "checking" | "authenticated" | "unauthenticated" | "error";

function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401;
}

export function AuthGate() {
  const { t } = useTranslation();
  const location = useLocation();
  const [state, setState] = useState<AuthState>("checking");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;

    async function verifySession() {
      setState("checking");
      try {
        // system.info is protected when authentication is enabled, but
        // remains reachable in the authentication-disabled demo topology.
        // One successful request therefore proves that this browser may mount
        // the protected application without adding a public auth-status API.
        await api.system.info();
        if (!cancelled) setState("authenticated");
        return;
      } catch (error) {
        if (!isUnauthorized(error)) {
          if (!cancelled) setState("error");
          return;
        }
      }

      // A short-lived access cookie may have expired while the seven-day
      // refresh cookie is still valid. Try the designed session-rotation path
      // once before deciding that the user must log in again.
      try {
        await api.auth.refresh();
      } catch (error) {
        if (!cancelled) {
          setState(isUnauthorized(error) ? "unauthenticated" : "error");
        }
        return;
      }

      try {
        await api.system.info();
        if (!cancelled) setState("authenticated");
      } catch (error) {
        if (!cancelled) {
          setState(isUnauthorized(error) ? "unauthenticated" : "error");
        }
      }
    }

    void verifySession();
    return () => {
      cancelled = true;
    };
  }, [attempt]);

  if (state === "checking") {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <div
          role="status"
          aria-label={t("authGate.checking")}
          className="h-8 w-8 animate-spin rounded-full border-4 border-muted border-t-primary"
        />
      </div>
    );
  }

  if (state === "unauthenticated") {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }

  if (state === "error") {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background p-6">
        <div className="max-w-sm text-center">
          <h1 className="mb-2 text-lg font-semibold text-destructive">
            {t("authGate.errorTitle")}
          </h1>
          <p className="mb-4 text-sm text-muted-foreground">
            {t("authGate.errorDescription")}
          </p>
          <Button onClick={() => setAttempt((value) => value + 1)}>
            {t("errorBoundary.retry")}
          </Button>
        </div>
      </div>
    );
  }

  // The session and its capability contract are read once here, for the whole
  // protected tree, rather than by each page that needs them.
  return (
    <SessionProvider>
      <Outlet />
    </SessionProvider>
  );
}
