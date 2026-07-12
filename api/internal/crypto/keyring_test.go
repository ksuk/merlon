package crypto

import (
	"encoding/base64"
	"os"
	"testing"
)

func testKey(b byte) string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestKeyRingParsesMultipleGenerations(t *testing.T) {
	spec := "v1:" + testKey(1) + ",v2:" + testKey(2)
	kr, err := ParseKeyRing(spec)
	if err != nil {
		t.Fatalf("ParseKeyRing: %v", err)
	}

	k1, err := kr.Key(1)
	if err != nil {
		t.Fatalf("Key(1): %v", err)
	}
	if len(k1) != 32 || k1[0] != 1 {
		t.Errorf("key v1 = %v, want 32 bytes of 0x01", k1)
	}

	k2, err := kr.Key(2)
	if err != nil {
		t.Fatalf("Key(2): %v", err)
	}
	if len(k2) != 32 || k2[0] != 2 {
		t.Errorf("key v2 = %v, want 32 bytes of 0x02", k2)
	}

	if kr.CurrentVersion() != 2 {
		t.Errorf("CurrentVersion() = %d, want 2 (highest version)", kr.CurrentVersion())
	}
}

func TestKeyRingFromEnv(t *testing.T) {
	const envVar = "MERLON_TEST_KEY_RING"
	t.Setenv(envVar, "v1:"+testKey(9))

	kr, err := NewKeyRingFromEnv(envVar)
	if err != nil {
		t.Fatalf("NewKeyRingFromEnv: %v", err)
	}
	if kr.CurrentVersion() != 1 {
		t.Errorf("CurrentVersion() = %d, want 1", kr.CurrentVersion())
	}
}

func TestKeyRingFromEnvMissing(t *testing.T) {
	os.Unsetenv("MERLON_TEST_KEY_RING_MISSING")
	if _, err := NewKeyRingFromEnv("MERLON_TEST_KEY_RING_MISSING"); err == nil {
		t.Error("expected error for unset env var, got nil")
	}
}

func TestKeyRingRejectsWrongKeyLength(t *testing.T) {
	shortKey := base64.StdEncoding.EncodeToString([]byte("too-short"))
	if _, err := ParseKeyRing("v1:" + shortKey); err == nil {
		t.Error("expected error for non-32-byte key, got nil")
	}
}

func TestKeyRingKeyUnknownVersion(t *testing.T) {
	kr, err := ParseKeyRing("v1:" + testKey(1))
	if err != nil {
		t.Fatalf("ParseKeyRing: %v", err)
	}
	if _, err := kr.Key(99); err == nil {
		t.Error("expected error for unknown key_version, got nil")
	}
}
