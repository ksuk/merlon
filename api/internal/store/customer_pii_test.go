package store

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/crypto"
	"github.com/merlon-aml/merlon/api/internal/domain"
)

func testEncryptor(t *testing.T) *crypto.Encryptor {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	kr, err := crypto.ParseKeyRing("v1:" + base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("ParseKeyRing: %v", err)
	}
	return crypto.NewEncryptor(kr)
}

// TestCustomerAttributesDirectPIIEncryptedAtRest verifies encryptDirectPII
// replaces every direct-PII field (data-model.md §3.1) with ciphertext,
// never leaving the plaintext value in what would be persisted.
func TestCustomerAttributesDirectPIIEncryptedAtRest(t *testing.T) {
	e := testEncryptor(t)
	attrs := map[string]any{
		"full_name":          "田中太郎",
		"address":            "東京都千代田区1-1-1",
		"date_of_birth":      "1980-01-01",
		"phone":              "090-1234-5678",
		"email":              "taro@example.com",
		"account_number":     "1234567890",
		"id_document_number": "AB1234567",
		"occupation":         "engineer",
	}

	encrypted, err := encryptDirectPII(e, attrs)
	if err != nil {
		t.Fatalf("encryptDirectPII: %v", err)
	}

	for _, key := range directPIIAttributeKeys {
		original := attrs[key].(string)
		got, ok := encrypted[key].(string)
		if !ok {
			t.Fatalf("encrypted[%q] = %T, want string", key, encrypted[key])
		}
		if got == original {
			t.Errorf("encrypted[%q] = %q, want ciphertext distinct from plaintext %q", key, got, original)
		}
	}
}

// TestCustomerAttributesQuasiPIIRemainsPlaintext verifies quasi-PII and AML
// risk attributes (§3.1: occupation, industry, nationality, residence
// country, PEP flag, etc.) are untouched by encryptDirectPII, since they
// stay plaintext for GIN indexing.
func TestCustomerAttributesQuasiPIIRemainsPlaintext(t *testing.T) {
	e := testEncryptor(t)
	attrs := map[string]any{
		"full_name":         "田中太郎",
		"occupation":        "engineer",
		"industry":          "technology",
		"nationality":       "JP",
		"residence_country": "JP",
		"is_pep":            true,
	}

	encrypted, err := encryptDirectPII(e, attrs)
	if err != nil {
		t.Fatalf("encryptDirectPII: %v", err)
	}

	for _, key := range []string{"occupation", "industry", "nationality", "residence_country", "is_pep"} {
		if encrypted[key] != attrs[key] {
			t.Errorf("encrypted[%q] = %v, want unchanged %v", key, encrypted[key], attrs[key])
		}
	}
}

// TestCustomerGetDecryptsTransparently verifies the encrypt -> decrypt
// round trip through the store-layer helpers returns the original
// plaintext (Get/List transparency, WS-11 Task 7).
func TestCustomerGetDecryptsTransparently(t *testing.T) {
	e := testEncryptor(t)
	original := map[string]any{
		"full_name": "山田花子",
		"email":     "hanako@example.com",
		"industry":  "finance",
	}

	encrypted, err := encryptDirectPII(e, original)
	if err != nil {
		t.Fatalf("encryptDirectPII: %v", err)
	}

	decryptDirectPII(e, encrypted)

	if encrypted["full_name"] != "山田花子" {
		t.Errorf("full_name = %v, want %q", encrypted["full_name"], "山田花子")
	}
	if encrypted["email"] != "hanako@example.com" {
		t.Errorf("email = %v, want %q", encrypted["email"], "hanako@example.com")
	}
	if encrypted["industry"] != "finance" {
		t.Errorf("industry = %v, want unchanged %q", encrypted["industry"], "finance")
	}
}

// TestDecryptDirectPIILeavesPreEncryptionPlaintextUntouched verifies
// enabling encryption doesn't break reads of rows written before it was
// configured: a value that fails to decrypt (plaintext, not ciphertext) is
// left as-is instead of erroring.
func TestDecryptDirectPIILeavesPreEncryptionPlaintextUntouched(t *testing.T) {
	e := testEncryptor(t)
	attrs := map[string]any{"full_name": "plaintext-legacy-value"}

	decryptDirectPII(e, attrs)

	if attrs["full_name"] != "plaintext-legacy-value" {
		t.Errorf("full_name = %v, want unchanged legacy plaintext", attrs["full_name"])
	}
}

func TestEncryptDirectPIINilEncryptorIsPassthrough(t *testing.T) {
	attrs := map[string]any{"full_name": "田中太郎"}
	out, err := encryptDirectPII(nil, attrs)
	if err != nil {
		t.Fatalf("encryptDirectPII: %v", err)
	}
	if out["full_name"] != "田中太郎" {
		t.Errorf("full_name = %v, want unchanged (nil encryptor passthrough)", out["full_name"])
	}
}

// TestPgCustomerEncryptionRoundTrip exercises PgCustomerRepo end to end
// against a live Postgres connection: direct PII is ciphertext in the raw
// row, quasi-PII stays plaintext, and Get/List decrypt transparently.
// Skipped when MERLON_DATABASE_URL is unset.
func TestPgCustomerEncryptionRoundTrip(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	e := testEncryptor(t)
	repo := NewPgCustomerRepo(pool, e)

	c := &domain.Customer{
		ID: newTestUUID(), ExternalID: "pii-roundtrip-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP",
		Status: domain.CustomerStatusActive,
		Attributes: map[string]any{
			"full_name":  "暗号化太郎",
			"occupation": "engineer",
		},
	}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, c.ID)
	})

	var rawAttrs []byte
	if err := pool.QueryRow(ctx, `SELECT attributes FROM customers WHERE id = $1`, c.ID).Scan(&rawAttrs); err != nil {
		t.Fatalf("query raw attributes: %v", err)
	}
	rawJSON := string(rawAttrs)
	if strings.Contains(rawJSON, "暗号化太郎") {
		t.Error("raw stored attributes contain plaintext full_name, want ciphertext")
	}
	if !strings.Contains(rawJSON, "engineer") {
		t.Error("raw stored attributes should keep quasi-PII (occupation) as plaintext")
	}

	got, err := repo.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Attributes["full_name"] != "暗号化太郎" {
		t.Errorf("decrypted full_name = %v, want %q", got.Attributes["full_name"], "暗号化太郎")
	}
}
