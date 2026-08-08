import type { KYCRequiredFieldsPolicy } from "@/lib/api"
import { identityRequirements } from "@/lib/identity"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"

const DATE_FIELDS = new Set(["date_of_birth"])

interface IdentityFieldsProps {
  customerType: string
  policy: KYCRequiredFieldsPolicy | null | undefined
  values: Record<string, string>
  onChange: (field: string, value: string) => void
  idPrefix: string
  fieldClassName?: string
}

// IdentityFields renders the identity inputs the kyc_required_fields policy
// asks for, for this customer type. Required fields carry aria-required but
// not the HTML required attribute: the shipped enforcement is `warn`, so the
// server accepts the record and reports the gap, and a form that refused to
// submit would be stricter than the policy it claims to implement.
export function IdentityFields({ customerType, policy, values, onChange, idPrefix, fieldClassName = "" }: IdentityFieldsProps) {
  const { t } = useTranslation()
  const { required, fields } = identityRequirements(policy, customerType)

  return (
    <>
      {fields.map((field) => {
        const id = `${idPrefix}-${field}`
        const isRequired = required.has(field)
        return (
          <div key={field} className={fieldClassName}>
            <label htmlFor={id} className="mb-1 block text-xs font-medium">
              {t(`customers.identityField.${field}`, { defaultValue: field })}
              {isRequired && <span className="ml-1 text-destructive" title={t("customers.identityField.requiredHint")}>*</span>}
            </label>
            <input
              id={id}
              type={DATE_FIELDS.has(field) ? "date" : "text"}
              aria-required={isRequired}
              value={values[field] ?? ""}
              onChange={(event) => onChange(field, event.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            />
          </div>
        )
      })}
    </>
  )
}

const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// The runtime has no list of ISO 3166-1 alpha-2 codes to ask for
// (Intl.supportedValuesOf takes no "region" key), so the two-letter space is
// enumerated and filtered by whether the locale data names the code.
// fallback: "none" makes of() return undefined for an unassigned code, which
// is the discriminator.
function isoRegionCodes(locale: string): string[] {
  const names = new Intl.DisplayNames([locale], { type: "region", fallback: "none" })
  const codes: string[] = []
  for (const first of ALPHABET) {
    for (const second of ALPHABET) {
      const code = `${first}${second}`
      if (names.of(code)) codes.push(code)
    }
  }
  return codes
}

interface CountrySelectProps {
  id: string
  value: string
  onChange: (value: string) => void
  label: string
  className?: string
}

// CountrySelect stores the ISO 3166-1 alpha-2 code and shows the localised
// region name. Free text let an operator save anything two characters long,
// which the country-risk tables then failed to match.
export function CountrySelect({ id, value, onChange, label, className = "" }: CountrySelectProps) {
  const { i18n } = useTranslation()
  const options = useMemo(() => {
    const names = new Intl.DisplayNames([i18n.language], { type: "region" })
    const regionCodes = isoRegionCodes(i18n.language)
    // A record saved before this select existed may hold a code the locale
    // data does not name; it is offered anyway so editing another field does
    // not silently rewrite the country.
    const codes = !value || regionCodes.includes(value) ? regionCodes : [value, ...regionCodes]
    return codes
      .map((code) => ({ code, label: names.of(code) ?? code }))
      .sort((left, right) => left.label.localeCompare(right.label, i18n.language))
  }, [i18n.language, value])

  return (
    <select
      id={id}
      aria-label={label}
      aria-required="true"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className={`rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring ${className}`}
    >
      <option value="">-</option>
      {options.map((option) => (
        <option key={option.code} value={option.code}>{option.label} ({option.code})</option>
      ))}
    </select>
  )
}
