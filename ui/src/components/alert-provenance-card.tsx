import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import type { AlertProvenance } from "@/lib/api"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

/**
 * AlertProvenanceCard shows what produced a detection.
 *
 * The visible scenario name and score were never enough to prove which logic
 * and inputs were effective when an alert fired, so after a rule changed an
 * analyst could not reproduce the finding (#84). This shows the identifiers
 * that make it reproducible, and says plainly when they are not available
 * rather than filling the gap with current configuration.
 */
export function AlertProvenanceCard({ provenance }: { provenance?: AlertProvenance }) {
  const { t } = useTranslation()

  if (!provenance) {
    return null
  }

  const notCaptured = provenance.availability === "not_captured"
  const missing = provenance.availability === "missing"

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2 text-base">
          {t("alertDetail.provenance.title")}
          <Badge variant={notCaptured || missing ? "secondary" : "outline"}>
            {t(`alertDetail.provenance.availability.${provenance.availability}`)}
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <p className="text-xs text-muted-foreground">
          {t(`alertDetail.provenance.explanation.${provenance.availability}`)}
        </p>

        <dl className="space-y-2">
          <div className="flex flex-wrap justify-between gap-2">
            <dt className="text-muted-foreground">{t("alertDetail.provenance.scenario")}</dt>
            <dd className="font-mono">{provenance.scenario_id}</dd>
          </div>

          {provenance.rule_name && provenance.rule_version !== undefined && (
            <div className="flex flex-wrap justify-between gap-2">
              <dt className="text-muted-foreground">{t("alertDetail.provenance.ruleVersion")}</dt>
              <dd>
                {/* Resolving the identifier is the reviewer's next step, and the
                    rule API applies its own authorization there. */}
                <Link
                  to={`/rules/${encodeURIComponent(provenance.rule_name)}`}
                  className="font-mono text-primary underline-offset-4 hover:underline"
                >
                  {provenance.rule_name} @ v{provenance.rule_version}
                </Link>
              </dd>
            </div>
          )}

          {provenance.rule_digest && (
            <div className="flex flex-wrap justify-between gap-2">
              <dt className="text-muted-foreground">{t("alertDetail.provenance.ruleDigest")}</dt>
              <dd className="break-all font-mono text-xs">{provenance.rule_digest.slice(0, 16)}</dd>
            </div>
          )}

          {provenance.evaluation_mode && (
            <div className="flex flex-wrap justify-between gap-2">
              <dt className="text-muted-foreground">{t("alertDetail.provenance.evaluationMode")}</dt>
              <dd>{provenance.evaluation_mode}</dd>
            </div>
          )}

          {provenance.applied_threshold !== undefined && (
            <div className="flex flex-wrap justify-between gap-2">
              <dt className="text-muted-foreground">{t("alertDetail.provenance.appliedThreshold")}</dt>
              <dd className="font-mono">{provenance.applied_threshold}</dd>
            </div>
          )}

          {provenance.evaluated_at && (
            <div className="flex flex-wrap justify-between gap-2">
              <dt className="text-muted-foreground">{t("alertDetail.provenance.evaluatedAt")}</dt>
              <dd>{provenance.evaluated_at}</dd>
            </div>
          )}

          {provenance.engine_version && (
            <div className="flex flex-wrap justify-between gap-2">
              <dt className="text-muted-foreground">{t("alertDetail.provenance.engineVersion")}</dt>
              <dd className="font-mono">{provenance.engine_version}</dd>
            </div>
          )}
        </dl>

        {provenance.config_digests && Object.keys(provenance.config_digests).length > 0 && (
          <div>
            <p className="mb-1 text-xs font-medium">{t("alertDetail.provenance.configDigests")}</p>
            <dl className="grid gap-1 sm:grid-cols-[14rem_1fr]">
              {Object.entries(provenance.config_digests).map(([name, digest]) => (
                <div key={name} className="contents">
                  <dt className="font-mono text-xs text-muted-foreground">{name}</dt>
                  <dd className="break-all font-mono text-xs">{digest}</dd>
                </div>
              ))}
            </dl>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
