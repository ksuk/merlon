import { useTranslation } from "react-i18next"
import { useCapability } from "@/hooks/use-session"
import { cn } from "@/lib/utils"

/**
 * CapabilityNotice explains an absent control.
 *
 * A permission-sensitive control that simply disappears reads as a product
 * limitation. The operator cannot tell whether their role lacks the grant,
 * the deployment never configured the function, or the capability lookup
 * itself failed — three different situations with three different responses
 * (#81). Rendering this where the control would have been turns a silent
 * removal into a stated reason.
 */
export function CapabilityNotice({
  capabilityId,
  className,
}: {
  capabilityId: string
  className?: string
}) {
  const { t } = useTranslation()
  const capability = useCapability(capabilityId)

  if (capability.availability === "available") {
    return null
  }

  const reason = t(`capability.reason.${capability.reason_code ?? "unknown_capability"}`, {
    defaultValue: "",
  })

  return (
    <p role="note" className={cn("text-xs text-muted-foreground", className)}>
      <span className="font-medium">{t(`capability.availability.${capability.availability}`)}</span>
      {reason ? ` — ${reason}` : null}
      {capability.docs_url ? (
        <>
          {" "}
          <a className="underline" href={capability.docs_url}>
            {t("capability.documentation")}
          </a>
        </>
      ) : null}
    </p>
  )
}
