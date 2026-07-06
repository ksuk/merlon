import type { TFunction } from "i18next"
import { ApiError } from "./api"

// translateApiError resolves a caught error to a user-facing message via the
// error_code -> errors.{code} i18n mapping (Contract Stability: the branch is
// on error_code, not the message string). Unknown codes and non-API errors
// fall back to the raw message, then to errors.unknown.
export function translateApiError(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    if (err.code) {
      return t(`errors.${err.code}`, { defaultValue: err.message || t("errors.unknown") })
    }
    return err.message || t("errors.unknown")
  }
  if (err instanceof Error) return err.message
  return t("errors.unknown")
}
