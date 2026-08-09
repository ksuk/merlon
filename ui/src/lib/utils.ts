import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// formatCountry renders an ISO 3166-1 alpha-2 code in the viewer's language.
// The code stays canonical everywhere it is stored or sent; only the display
// is localized. Three pages had their own copy of this, and the transaction
// screens had none, so a counterparty country showed as "SG" next to a
// customer country that read "シンガポール".
export function formatCountry(code: string, locale: string): string {
  if (!code) return code
  try {
    return new Intl.DisplayNames([locale], { type: "region" }).of(code) ?? code
  } catch {
    return code
  }
}

// formatDuration renders a second count as the largest whole unit that fits.
// Screening freshness is judged in hours and days, so a bare second count
// leaves an operator doing arithmetic to decide whether a list is stale.
export function formatDuration(seconds: number, t: (key: string, opts?: Record<string, unknown>) => string): string {
  const abs = Math.max(0, Math.round(seconds))
  // Days only from 48h. Screening freshness windows are measured in days but
  // breached in hours, so rounding 26 hours to "1 day" hides how close to the
  // threshold a source is -- and reads as fresher than it is.
  if (abs >= 172800) return t("duration.days", { count: Math.round(abs / 86400) })
  if (abs >= 3600) return t("duration.hours", { count: Math.round(abs / 3600) })
  return t("duration.minutes", { count: Math.round(abs / 60) })
}

// customerStatusVariant maps a lifecycle state onto a badge colour.
//
// Every state rendered as `outline` before, so a frozen, dormant or closed
// customer was visually identical to an active one -- the exact confusion #75
// asks to remove, on a field that decides whether the customer is evaluated at
// all.
export function customerStatusVariant(
  status: string | undefined,
): "low" | "medium" | "critical" | "secondary" {
  switch (status ?? "active") {
    case "active":
      return "low"
    case "dormant":
      return "secondary"
    case "frozen":
      return "critical"
    case "closed":
      return "medium"
    default:
      return "secondary"
  }
}
