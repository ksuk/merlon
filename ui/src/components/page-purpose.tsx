import { useTranslation } from "react-i18next"
import { useCapability } from "@/hooks/use-session"

/**
 * PagePurpose states what an administrative screen is for before an operator
 * uses it.
 *
 * API Keys and Webhooks were reachable from the sidebar with no statement of
 * their intended consumers, prerequisites, data flow, retry behaviour or the
 * consequence of removing one (#80). An administrative control an operator has
 * to reverse-engineer is one they can misuse without noticing.
 *
 * The capability's availability is shown alongside, so "this deployment has not
 * configured it" is answered here rather than by a failing request.
 */
export function PagePurpose({
  capabilityId,
  bodyKey,
  points,
}: {
  capabilityId: string
  /** i18n key for the one-paragraph statement of purpose. */
  bodyKey: string
  /** i18n keys for the operational facts an operator needs before acting. */
  points: string[]
}) {
  const { t } = useTranslation()
  const capability = useCapability(capabilityId)

  return (
    <section
      aria-label={t("capability.purposeLabel")}
      className="rounded-lg border bg-muted/30 p-4 text-sm"
    >
      <p className="text-muted-foreground">{t(bodyKey)}</p>
      <ul className="mt-2 list-disc space-y-1 pl-5 text-xs text-muted-foreground">
        {points.map((key) => (
          <li key={key}>{t(key)}</li>
        ))}
      </ul>
      <p className="mt-3 text-xs">
        <span className="font-medium">{t("capability.stateLabel")}: </span>
        {t(`capability.availability.${capability.availability}`)}
        {capability.reason_code
          ? ` — ${t(`capability.reason.${capability.reason_code}`, { defaultValue: "" })}`
          : null}
        {capability.docs_url ? (
          <>
            {" "}
            <a className="underline" href={capability.docs_url}>
              {t("capability.documentation")}
            </a>
          </>
        ) : null}
      </p>
    </section>
  )
}
