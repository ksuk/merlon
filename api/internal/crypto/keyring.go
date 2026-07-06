// Package crypto implements AES-256-GCM field encryption with key
// versioning (security.md §2.1, WS-11 Task 6).
package crypto

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const keySize = 32 // AES-256

// KeyRing holds every key generation still needed to decrypt existing data,
// keyed by the single-byte key_version embedded in each ciphertext.
// Encryption always uses CurrentVersion(); decryption looks up whatever
// version the ciphertext itself carries, so old ciphertexts stay readable
// across a rotation as long as their key remains in the ring.
type KeyRing struct {
	keys    map[uint8][]byte
	current uint8
}

// NewKeyRingFromEnv reads envVar and parses it as a comma-separated
// "vN:base64key" list (e.g. "v1:base64key,v2:base64key").
func NewKeyRingFromEnv(envVar string) (*KeyRing, error) {
	spec := os.Getenv(envVar)
	if spec == "" {
		return nil, fmt.Errorf("crypto: environment variable %s not set", envVar)
	}
	return ParseKeyRing(spec)
}

// ParseKeyRing parses a comma-separated "vN:base64key" list. The highest
// numbered version becomes CurrentVersion() -- the version new encryptions
// use.
func ParseKeyRing(spec string) (*KeyRing, error) {
	keys := make(map[uint8][]byte)
	var current uint8
	var sawAny bool

	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		versionPart, keyPart, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("crypto: invalid key ring entry %q, want \"vN:base64key\"", entry)
		}
		versionPart = strings.TrimPrefix(versionPart, "v")
		version, err := strconv.ParseUint(versionPart, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("crypto: invalid key version %q: %w", versionPart, err)
		}
		key, err := base64.StdEncoding.DecodeString(keyPart)
		if err != nil {
			return nil, fmt.Errorf("crypto: invalid base64 key for version %d: %w", version, err)
		}
		if len(key) != keySize {
			return nil, fmt.Errorf("crypto: key for version %d must be %d bytes (AES-256), got %d", version, keySize, len(key))
		}

		v := uint8(version)
		keys[v] = key
		if !sawAny || v > current {
			current = v
		}
		sawAny = true
	}

	if !sawAny {
		return nil, fmt.Errorf("crypto: no keys found in key ring spec")
	}
	return &KeyRing{keys: keys, current: current}, nil
}

// CurrentVersion is the key_version new Encrypt calls embed and use.
func (k *KeyRing) CurrentVersion() uint8 {
	return k.current
}

// Key returns the 32-byte key for version, or an error if the ring doesn't
// hold it (e.g. it was retired past the rotation's retention window).
func (k *KeyRing) Key(version uint8) ([]byte, error) {
	key, ok := k.keys[version]
	if !ok {
		return nil, fmt.Errorf("crypto: unknown key_version %d", version)
	}
	return key, nil
}
