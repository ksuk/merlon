package domain

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// NormalizeUUID accepts both the compact representation emitted by the API
// and the canonical hyphenated representation returned by PostgreSQL. It
// returns the one public representation used by domain objects and JSON.
// Callers that handle non-UUID TEXT identifiers must keep those identifiers
// separate and use CanonicalIdentifier instead.
func NormalizeUUID(value string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(raw, "-") {
		parts := strings.Split(raw, "-")
		wantLengths := [...]int{8, 4, 4, 4, 12}
		if len(parts) != len(wantLengths) {
			return "", fmt.Errorf("invalid UUID %q", value)
		}
		for i, part := range parts {
			if len(part) != wantLengths[i] {
				return "", fmt.Errorf("invalid UUID %q", value)
			}
		}
	}
	compact := strings.ReplaceAll(raw, "-", "")
	if len(compact) != 32 {
		return "", fmt.Errorf("invalid UUID %q", value)
	}
	if _, err := hex.DecodeString(compact); err != nil {
		return "", fmt.Errorf("invalid UUID %q: %w", value, err)
	}
	return compact, nil
}

// CanonicalUUID is the tolerant read-boundary form. Invalid values are
// returned trimmed so legacy TEXT identifiers remain observable and can be
// rejected by the operation that owns their validation instead of being
// silently rewritten.
func CanonicalUUID(value string) string {
	if normalized, err := NormalizeUUID(value); err == nil {
		return normalized
	}
	return strings.TrimSpace(value)
}

// CanonicalIdentifier compares UUID-backed references without making a
// non-UUID TEXT identifier look like a UUID.
func CanonicalIdentifier(value string) string {
	return CanonicalUUID(value)
}

func SameIdentifier(left, right string) bool {
	return CanonicalIdentifier(left) == CanonicalIdentifier(right)
}
