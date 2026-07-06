package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/merlon-aml/merlon/api/internal/crypto"
)

func testKeyRing(t *testing.T, versions ...uint8) *crypto.KeyRing {
	t.Helper()
	spec := ""
	for i, v := range versions {
		if i > 0 {
			spec += ","
		}
		key := make([]byte, 32)
		for j := range key {
			key[j] = v
		}
		spec += "v" + strconv.Itoa(int(v)) + ":" + base64.StdEncoding.EncodeToString(key)
	}
	kr, err := crypto.ParseKeyRing(spec)
	if err != nil {
		t.Fatalf("ParseKeyRing: %v", err)
	}
	return kr
}

func TestNeedsRotationDetectsStaleVersion(t *testing.T) {
	kr := testKeyRing(t, 1, 2)
	e := crypto.NewEncryptor(kr)

	// kr's current version is 2 (highest), so encrypting now embeds v2.
	ciphertext, err := e.Encrypt("payload")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	stale, err := needsRotation(ciphertext, 2)
	if err != nil {
		t.Fatalf("needsRotation: %v", err)
	}
	if stale {
		t.Error("needsRotation = true for data already under the current version, want false")
	}

	stale, err = needsRotation(ciphertext, 3)
	if err != nil {
		t.Fatalf("needsRotation: %v", err)
	}
	if !stale {
		t.Error("needsRotation = false for data under a stale version, want true")
	}
}

// TestRotateAttributesReencryptsStaleFieldsOnly verifies rotateAttributes
// re-encrypts only direct-PII fields under a stale key_version, leaves
// already-current fields untouched, and leaves non-PII fields alone
// entirely -- the pure logic merlon-keyrotate's RotateAll batches over
// live Postgres rows (Pg-backed test in TestKeyRotateReencryptsExistingRecords
// below verifies the full path).
func TestRotateAttributesReencryptsStaleFieldsOnly(t *testing.T) {
	oldKR := testKeyRing(t, 1)
	oldEncryptor := crypto.NewEncryptor(oldKR)

	staleCiphertext, err := oldEncryptor.Encrypt("田中太郎")
	if err != nil {
		t.Fatalf("Encrypt under old key: %v", err)
	}

	newKR := testKeyRing(t, 1, 2)
	newEncryptor := crypto.NewEncryptor(newKR)
	currentCiphertext, err := newEncryptor.Encrypt("鈴木花子")
	if err != nil {
		t.Fatalf("Encrypt under current key: %v", err)
	}

	attrs := map[string]any{
		"full_name":  staleCiphertext,
		"email":      currentCiphertext,
		"occupation": "engineer",
	}

	rotated, changed, err := rotateAttributes(newEncryptor, newKR.CurrentVersion(), attrs)
	if err != nil {
		t.Fatalf("rotateAttributes: %v", err)
	}
	if !changed {
		t.Fatal("expected changed = true (full_name was stale)")
	}

	if rotated["full_name"] == staleCiphertext {
		t.Error("full_name was not re-encrypted")
	}
	decrypted, err := newEncryptor.Decrypt(rotated["full_name"].(string))
	if err != nil {
		t.Fatalf("decrypt rotated full_name: %v", err)
	}
	if decrypted != "田中太郎" {
		t.Errorf("rotated full_name decrypts to %q, want %q", decrypted, "田中太郎")
	}

	if rotated["email"] != currentCiphertext {
		t.Error("email (already current) should not have been re-encrypted")
	}
	if rotated["occupation"] != "engineer" {
		t.Error("non-PII field should be untouched")
	}
}

func TestRotateAttributesNoChangeWhenAllCurrent(t *testing.T) {
	kr := testKeyRing(t, 1)
	e := crypto.NewEncryptor(kr)
	ciphertext, err := e.Encrypt("payload")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, changed, err := rotateAttributes(e, kr.CurrentVersion(), map[string]any{"full_name": ciphertext})
	if err != nil {
		t.Fatalf("rotateAttributes: %v", err)
	}
	if changed {
		t.Error("expected changed = false when every field is already under the current key version")
	}
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("pool.Ping: %v", err)
	}
	return pool
}

// TestKeyRotateReencryptsExistingRecords, TestKeyRotateIsIdempotentAndResumable,
// and TestKeyRotateDoesNotCauseDowntime require a live Postgres connection
// (skipped when MERLON_DATABASE_URL is unset).
func TestKeyRotateReencryptsExistingRecords(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	oldKR := testKeyRing(t, 1)
	oldEncryptor := crypto.NewEncryptor(oldKR)
	ciphertext, err := oldEncryptor.Encrypt("keyrotate-target")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	var id string
	err = pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		VALUES ($1, 'individual', 'JP', '{}', $2) RETURNING id`,
		"keyrotate-"+base64.StdEncoding.EncodeToString([]byte(t.Name())),
		[]byte(`{"full_name":"`+ciphertext+`","occupation":"engineer"}`),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test customer: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id) })

	newKR := testKeyRing(t, 1, 2)
	newEncryptor := crypto.NewEncryptor(newKR)

	reencrypted, err := RotateAll(ctx, pool, newEncryptor, newKR.CurrentVersion(), 500)
	if err != nil {
		t.Fatalf("RotateAll: %v", err)
	}
	if reencrypted == 0 {
		t.Error("expected at least one row re-encrypted")
	}

	var rawAttrs []byte
	if err := pool.QueryRow(ctx, `SELECT attributes FROM customers WHERE id = $1`, id).Scan(&rawAttrs); err != nil {
		t.Fatalf("query attributes: %v", err)
	}
	var attrs map[string]any
	if err := json.Unmarshal(rawAttrs, &attrs); err != nil {
		t.Fatalf("unmarshal attributes: %v", err)
	}
	stale, err := needsRotation(attrs["full_name"].(string), newKR.CurrentVersion())
	if err != nil {
		t.Fatalf("needsRotation: %v", err)
	}
	if stale {
		t.Error("full_name should be re-encrypted under the new current key_version")
	}

	decrypted, err := newEncryptor.Decrypt(attrs["full_name"].(string))
	if err != nil {
		t.Fatalf("decrypt rotated value: %v", err)
	}
	if decrypted != "keyrotate-target" {
		t.Errorf("rotated value decrypts to %q, want %q", decrypted, "keyrotate-target")
	}
}

func TestKeyRotateIsIdempotentAndResumable(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	oldKR := testKeyRing(t, 1)
	oldEncryptor := crypto.NewEncryptor(oldKR)
	ciphertext, err := oldEncryptor.Encrypt("idempotent-target")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	var id string
	err = pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		VALUES ($1, 'individual', 'JP', '{}', $2) RETURNING id`,
		"keyrotate-idempotent-"+base64.StdEncoding.EncodeToString([]byte(t.Name())),
		[]byte(`{"full_name":"`+ciphertext+`"}`),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test customer: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id) })

	newKR := testKeyRing(t, 1, 2)
	newEncryptor := crypto.NewEncryptor(newKR)

	first, err := RotateAll(ctx, pool, newEncryptor, newKR.CurrentVersion(), 500)
	if err != nil {
		t.Fatalf("first RotateAll: %v", err)
	}
	if first == 0 {
		t.Fatal("expected first run to re-encrypt at least one row")
	}

	second, err := RotateAll(ctx, pool, newEncryptor, newKR.CurrentVersion(), 500)
	if err != nil {
		t.Fatalf("second RotateAll: %v", err)
	}
	if second != 0 {
		t.Errorf("second run re-encrypted %d rows, want 0 (already current)", second)
	}
}

func TestKeyRotateDoesNotCauseDowntime(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	oldKR := testKeyRing(t, 1)
	oldEncryptor := crypto.NewEncryptor(oldKR)
	ciphertext, err := oldEncryptor.Encrypt("mixed-key-target")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	var id string
	err = pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		VALUES ($1, 'individual', 'JP', '{}', $2) RETURNING id`,
		"keyrotate-downtime-"+base64.StdEncoding.EncodeToString([]byte(t.Name())),
		[]byte(`{"full_name":"`+ciphertext+`"}`),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test customer: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id) })

	// Both old and new key remain in the ring during rotation -- reads must
	// keep working against unrotated (old-key) data throughout.
	mixedKR := testKeyRing(t, 1, 2)
	mixedEncryptor := crypto.NewEncryptor(mixedKR)

	var rawBefore []byte
	if err := pool.QueryRow(ctx, `SELECT attributes FROM customers WHERE id = $1`, id).Scan(&rawBefore); err != nil {
		t.Fatalf("query attributes before rotation: %v", err)
	}
	var attrsBefore map[string]any
	if err := json.Unmarshal(rawBefore, &attrsBefore); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := mixedEncryptor.Decrypt(attrsBefore["full_name"].(string)); err != nil {
		t.Fatalf("expected old-key data to remain readable before rotation completes: %v", err)
	}

	if _, err := RotateAll(ctx, pool, mixedEncryptor, mixedKR.CurrentVersion(), 500); err != nil {
		t.Fatalf("RotateAll: %v", err)
	}

	var rawAfter []byte
	if err := pool.QueryRow(ctx, `SELECT attributes FROM customers WHERE id = $1`, id).Scan(&rawAfter); err != nil {
		t.Fatalf("query attributes after rotation: %v", err)
	}
	var attrsAfter map[string]any
	if err := json.Unmarshal(rawAfter, &attrsAfter); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	decrypted, err := mixedEncryptor.Decrypt(attrsAfter["full_name"].(string))
	if err != nil {
		t.Fatalf("expected data to remain readable after rotation: %v", err)
	}
	if decrypted != "mixed-key-target" {
		t.Errorf("decrypted = %q, want %q", decrypted, "mixed-key-target")
	}
}
