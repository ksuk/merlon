import type { KYCRequiredFieldsPolicy } from "@/lib/api"

// Fields the application renders for its own presentation (the customer list
// and the detail header read name_ja first) rather than because 取引時確認
// requires them. They are appended after the policy's own fields so an
// existing record stays editable, and they are never marked required.
const PRESENTATION_FIELDS = ["name_ja"]

// identityRequirements resolves the requirement set for one customer type.
// A type with no entry falls back to the policy defaults, which is the same
// resolution the server performs, so a form asks for exactly what the server
// will report as missing.
export function identityRequirements(
  policy: KYCRequiredFieldsPolicy | null | undefined,
  customerType: string,
): { required: Set<string>; fields: string[] } {
  const requirements = policy?.types?.[customerType] ?? policy?.defaults
  const required = requirements?.required ?? []
  const recommended = requirements?.recommended ?? []
  const fields = [...required, ...recommended]
  for (const field of PRESENTATION_FIELDS) {
    if (!fields.includes(field)) fields.push(field)
  }
  return { required: new Set(required), fields }
}
