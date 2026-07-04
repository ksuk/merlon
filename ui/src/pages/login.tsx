import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import { useRef, useState } from "react"
import { useNavigate } from "react-router-dom"

export function LoginPage() {
  const emailRef = useRef<HTMLInputElement>(null)
  const passwordRef = useRef<HTMLInputElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const navigate = useNavigate()

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const email = emailRef.current?.value.trim()
    const password = passwordRef.current?.value ?? ""
    if (!email || !password) return

    setError(null)
    setSubmitting(true)
    try {
      await api.auth.login(email, password)
      navigate("/")
    } catch {
      setError("メールアドレスまたはパスワードが正しくありません")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">ログイン</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label htmlFor="login-email" className="mb-1 block text-sm font-medium">
                メールアドレス
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
              <label htmlFor="login-password" className="mb-1 block text-sm font-medium">
                パスワード
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
              ログイン
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
