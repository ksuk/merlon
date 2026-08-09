/**
 * Shared operator-facing formatters.
 *
 * formatDateTime existed as fifteen identical local copies with no import
 * between them, formatAmount as three, and formatAge as two with different
 * signatures — so the same value could render differently on two screens and
 * nothing would catch it (#86). These are the one definition each.
 *
 * Every formatter keeps the canonical value reachable rather than replacing it:
 * an audit reader needs the ISO timestamp and the raw code, not only the
 * rendering.
 */

/** Renders a timestamp in the operator's locale. */
export function formatDateTime(iso: string | undefined | null, locale: string): string {
  if (!iso) return "-"
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) {
    // An unparseable value is shown as it arrived. Substituting a placeholder
    // would hide a data problem behind a plausible-looking dash.
    return iso
  }
  return date.toLocaleString(locale)
}

/** Renders a date without the time of day. */
export function formatDate(iso: string | undefined | null, locale: string): string {
  if (!iso) return "-"
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleDateString(locale)
}

/**
 * Renders a monetary amount in its own currency.
 *
 * The currency is never assumed: a figure shown in the wrong unit is worse than
 * one shown as a bare number.
 */
export function formatAmount(amount: number, currency: string, locale: string): string {
  if (!currency) return new Intl.NumberFormat(locale).format(amount)
  try {
    return new Intl.NumberFormat(locale, { style: "currency", currency }).format(amount)
  } catch {
    // An unknown currency code is not a reason to render nothing.
    return `${new Intl.NumberFormat(locale).format(amount)} ${currency}`
  }
}

/**
 * Renders how long ago a timestamp was, in whole days and hours.
 *
 * `now` is a parameter rather than Date.now() so a render stays pure: a
 * component that read the clock during render produced a different result on
 * every re-render with no new data.
 */
export function formatAge(iso: string | undefined | null, locale: string, now: number): string {
  if (!iso) return "-"
  const at = new Date(iso).getTime()
  if (Number.isNaN(at)) return iso
  const seconds = Math.max(0, Math.floor((now - at) / 1000))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  if (days > 0) return new Intl.NumberFormat(locale).format(days) + "d " + hours + "h"
  const minutes = Math.floor((seconds % 3600) / 60)
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}
