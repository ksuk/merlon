package store

import "github.com/merlon-aml/merlon/api/internal/crypto"

// directPIIAttributeKeys lists the customers.attributes keys classified as
// direct PII (data-model.md §3.1) and therefore encrypted at rest.
// Quasi-PII (occupation, industry, nationality, residence country) and AML
// risk attributes (PEP flag, entity type, etc.) stay plaintext, per §3.1, so
// GIN indexing over attributes keeps working for them.
var directPIIAttributeKeys = []string{
	"full_name",
	"address",
	"date_of_birth",
	"phone",
	"email",
	"account_number",
	"id_document_number",
}

// encryptDirectPII returns a shallow copy of attrs with every direct-PII
// string value replaced by its ciphertext. A nil encryptor (encryption not
// configured) is a no-op passthrough. Non-string values (e.g.
// attributes.trust_parties, WS-11 Task 3) are left untouched: direct PII
// fields are always plain strings per the §3.1 key list.
func encryptDirectPII(e *crypto.Encryptor, attrs map[string]any) (map[string]any, error) {
	if e == nil || attrs == nil {
		return attrs, nil
	}
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	for _, key := range directPIIAttributeKeys {
		s, ok := out[key].(string)
		if !ok || s == "" {
			continue
		}
		ciphertext, err := e.Encrypt(s)
		if err != nil {
			return nil, err
		}
		out[key] = ciphertext
	}
	return out, nil
}

// decryptDirectPII decrypts attrs' direct-PII fields in place. A value that
// fails to decrypt (e.g. plaintext written before encryption was
// configured) is left as-is rather than erroring, so turning on encryption
// doesn't break reads of pre-existing rows.
func decryptDirectPII(e *crypto.Encryptor, attrs map[string]any) {
	if e == nil || attrs == nil {
		return
	}
	for _, key := range directPIIAttributeKeys {
		s, ok := attrs[key].(string)
		if !ok || s == "" {
			continue
		}
		if plaintext, err := e.Decrypt(s); err == nil {
			attrs[key] = plaintext
		}
	}
}
