import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, type To, useLocation, useNavigate } from "react-router";

function postLoginDestination(state: unknown): To {
  if (!state || typeof state !== "object" || !("from" in state)) {
    return "/";
  }

  const from = state.from;
  if (!from || typeof from !== "object" || !("pathname" in from)) {
    return "/";
  }

  const pathname = from.pathname;
  if (
    typeof pathname !== "string" ||
    !pathname.startsWith("/") ||
    pathname.startsWith("//") ||
    pathname === "/login"
  ) {
    return "/";
  }

  return {
    pathname,
    search:
      "search" in from && typeof from.search === "string" ? from.search : "",
    hash: "hash" in from && typeof from.hash === "string" ? from.hash : "",
  };
}

export function LoginPage() {
  const { t } = useTranslation();
  const emailRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const email = emailRef.current?.value.trim();
    const password = passwordRef.current?.value ?? "";
    if (!email || !password) return;

    setError(null);
    setSubmitting(true);
    try {
      await api.auth.login(email, password);
      navigate(postLoginDestination(location.state), { replace: true });
    } catch {
      setError(t("login.error"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">{t("login.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label
                htmlFor="login-email"
                className="mb-1 block text-sm font-medium"
              >
                {t("login.emailLabel")}
              </label>
              <input
                id="login-email"
                ref={emailRef}
                type="email"
                required
                autoComplete="username"
                className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            <div>
              <label
                htmlFor="login-password"
                className="mb-1 block text-sm font-medium"
              >
                {t("login.passwordLabel")}
              </label>
              <input
                id="login-password"
                ref={passwordRef}
                type="password"
                required
                autoComplete="current-password"
                className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" className="w-full" disabled={submitting}>
              {t("login.submit")}
            </Button>
          </form>
          {/*
            A first-run deployment has no account to log in with, and the setup
            route is otherwise unreachable without knowing the URL. The link is
            shown unconditionally rather than probed for, because asking the
            server whether any user exists would disclose that to anyone who
            can reach the login page. Following it once an account exists is
            harmless: POST /api/v1/setup rejects the request.
          */}
          <p className="mt-4 text-center text-sm text-muted-foreground">
            {t("login.setupPrompt")}{" "}
            <Link
              to="/setup"
              className="font-medium text-foreground underline underline-offset-4"
            >
              {t("login.setupLink")}
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
