package crypto

import (
	"encoding/base64"
	"testing"
)

func newTestEncryptor(t *testing.T, spec string) *Encryptor {
	t.Helper()
	kr, err := ParseKeyRing(spec)
	if err != nil {
		t.Fatalf("ParseKeyRing: %v", err)
	}
	return NewEncryptor(kr)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	e := newTestEncryptor(t, "v1:"+testKey(1))

	plaintext := "田中太郎"
	ciphertext, err := e.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	decrypted, err := e.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptEmbedsCurrentKeyVersion(t *testing.T) {
	e := newTestEncryptor(t, "v1:"+testKey(1)+",v3:"+testKey(3))

	ciphertext, err := e.Encrypt("payload")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw[0] != 3 {
		t.Errorf("embedded key_version = %d, want 3 (current)", raw[0])
	}
}

func TestDecryptSelectsKeyByEmbeddedVersion(t *testing.T) {
	e := newTestEncryptor(t, "v1:"+testKey(1)+",v2:"+testKey(2))

	ciphertext, err := e.Encrypt("payload")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	decrypted, err := e.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != "payload" {
		t.Errorf("decrypted = %q, want %q", decrypted, "payload")
	}
}

// TestDecryptOldKeyVersionAfterRotation encrypts under v1, then constructs a
// new Encryptor whose KeyRing has rotated to v2 as current but still retains
// v1, verifying old data remains decryptable (security.md §2.1).
func TestDecryptOldKeyVersionAfterRotation(t *testing.T) {
	before := newTestEncryptor(t, "v1:"+testKey(1))
	ciphertext, err := before.Encrypt("legacy-data")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	after := newTestEncryptor(t, "v1:"+testKey(1)+",v2:"+testKey(2))
	decrypted, err := after.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt after rotation: %v", err)
	}
	if decrypted != "legacy-data" {
		t.Errorf("decrypted = %q, want %q", decrypted, "legacy-data")
	}
}

func TestDecryptFailsForUnknownKeyVersion(t *testing.T) {
	encryptor := newTestEncryptor(t, "v5:"+testKey(5))
	ciphertext, err := encryptor.Encrypt("payload")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	limited := newTestEncryptor(t, "v1:"+testKey(1))
	if _, err := limited.Decrypt(ciphertext); err == nil {
		t.Error("expected error decrypting with a key ring missing the embedded version, got nil")
	}
}

func TestEncryptProducesDifferentCiphertextForSamePlaintext(t *testing.T) {
	e := newTestEncryptor(t, "v1:"+testKey(1))

	c1, err := e.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	c2, err := e.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if c1 == c2 {
		t.Error("expected different ciphertexts for the same plaintext (nonce must be unique per call)")
	}
}
