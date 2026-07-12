import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { api, type ConfigValidationResult } from "@/lib/api"
import { CheckCircle, Settings2, XCircle } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"

export function ConfigPage() {
  const { t } = useTranslation()
  const configTypes = [
    { value: "cdd_weights", label: t("config.types.cddWeights") },
    { value: "screening_lists", label: t("config.types.screeningLists") },
    { value: "scenario_rules", label: t("config.types.scenarioRules") },
  ]
  const [configType, setConfigType] = useState("cdd_weights")
  const yamlRef = useRef<HTMLTextAreaElement>(null)
  const [validating, setValidating] = useState(false)
  const [result, setResult] = useState<ConfigValidationResult | null>(null)

  async function handleValidate(e: React.FormEvent) {
    e.preventDefault()
    const yaml = yamlRef.current?.value.trim()
    if (!yaml) return
    setValidating(true)
    setResult(null)
    try {
      const res = await api.config.validate(configType, yaml)
      setResult(res)
    } finally {
      setValidating(false)
    }
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("config.title")}</h1>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Settings2 className="h-4 w-4" />
            {t("config.form.title")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleValidate} className="space-y-4">
            <div>
              <label className="mb-2 block text-sm font-medium">{t("config.form.typeLabel")}</label>
              <div className="flex gap-2">
                {configTypes.map((ct) => (
                  <button
                    key={ct.value}
                    type="button"
                    onClick={() => setConfigType(ct.value)}
                    className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                      configType === ct.value
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-input text-muted-foreground hover:bg-accent"
                    }`}
                  >
                    {ct.label}
                  </button>
                ))}
              </div>
            </div>
            <div>
              <label className="mb-1 block text-sm font-medium">{t("config.form.yamlLabel")}</label>
              <textarea
                ref={yamlRef}
                required
                rows={12}
                placeholder={t("config.form.yamlPlaceholder")}
                className="w-full rounded-md border bg-background px-3 py-2 font-mono text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            <Button type="submit" size="sm" disabled={validating}>
              {validating ? t("config.form.validating") : t("config.form.submit")}
            </Button>
          </form>
        </CardContent>
      </Card>

      {result && (
        <Card className={result.valid ? "border-green-200" : "border-red-200"}>
          <CardContent className="p-4">
            <div className="flex items-center gap-2 mb-3">
              {result.valid ? (
                <>
                  <CheckCircle className="h-5 w-5 text-green-600" />
                  <span className="font-medium text-green-800">{t("config.result.successTitle")}</span>
                  <Badge variant="low">{t("config.result.validBadge")}</Badge>
                </>
              ) : (
                <>
                  <XCircle className="h-5 w-5 text-red-600" />
                  <span className="font-medium text-red-800">{t("config.result.errorTitle")}</span>
                  <Badge variant="destructive">{t("config.result.errorCount", { count: result.errors.length })}</Badge>
                </>
              )}
            </div>
            {result.errors.length > 0 && (
              <ul className="space-y-2">
                {result.errors.map((err, i) => (
                  <li key={i} className="rounded-md bg-red-50 p-3 text-sm">
                    <span className="font-mono text-xs text-red-600">{err.field}</span>
                    <span className="mx-2 text-red-300">—</span>
                    <span className="text-red-800">{err.message}</span>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
