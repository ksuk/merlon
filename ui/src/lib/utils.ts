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
