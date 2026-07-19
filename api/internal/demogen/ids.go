package demogen

import (
	"crypto/sha1" //nolint:gosec // RFC 4122 v5 UUIDs are defined over SHA-1; this is not a security use of the hash.
	"encoding/hex"
	"fmt"
)

// demoNamespaceUUID is a fixed, arbitrary UUID used as the RFC 4122 v5
// namespace for every demogen entity ID (uuidFor below). It was generated
// once (`uuidgen`) and is frozen here forever: changing it would change
// every derived ID on the next regeneration, breaking STORY_IDS.md's
// label/UUID mapping and any bookmarked demo-tour URLs. It carries no
// meaning beyond being a stable seed distinguishing this package's UUIDs
// from any other v5 namespace.
const demoNamespaceUUID = "5d1e9c3a-6b8f-4b7e-9d2a-8f6c4e2b7a1d"

var demoNamespaceBytes = mustParseUUID(demoNamespaceUUID)

// mustParseUUID decodes a canonical 8-4-4-4-12 hyphenated UUID string into
// its 16 raw bytes. It panics on malformed input, which only happens if
// demoNamespaceUUID itself is ever hand-edited incorrectly (there is no
// runtime input path to this function).
func mustParseUUID(s string) [16]byte {
	clean := make([]byte, 0, 32)
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			clean = append(clean, s[i])
		}
	}
	if len(clean) != 32 {
		panic("demogen: invalid fixed namespace UUID constant " + s)
	}
	var out [16]byte
	if _, err := hex.Decode(out[:], clean); err != nil {
		panic("demogen: invalid fixed namespace UUID constant " + s + ": " + err.Error())
	}
	return out
}

// uuidFor deterministically derives an RFC 4122 version-5 (SHA-1,
// namespace+name) UUID from label. The same label always yields the same
// UUID (regenerating the demo dataset is byte-identical, D-c /
// Auditability First), so every demogen entity keeps its human-readable
// generation-time label (e.g. "demo-story-01", "demo-txn-0000001") as the
// input to this function rather than needing a separate lookup table: a
// cross-reference (e.g. an alert's customer_id) is simply uuidFor of the
// same label used for the referenced entity's own id.
//
// This exists because PostgreSQL's UUID-typed primary key columns
// (customers, transactions, alerts, accounts, rule_definitions,
// customer_score_history — migrations/001, 002, 004, 011, 020) reject
// demogen's original human-readable IDs outright ("invalid input syntax
// for type uuid", confirmed against a real postgres:16.14-alpine server
// during PH7 T2). remap.go's remapIDsToUUIDs is the only caller that writes
// uuidFor's output into the actual generated entities; everywhere else in
// this package (self-checks, story wiring, STORY_IDS.md authoring) keeps
// working with the plain labels for readability.
func uuidFor(label string) string {
	h := sha1.New() //nolint:gosec // see the import comment above
	h.Write(demoNamespaceBytes[:])
	h.Write([]byte(label))
	sum := h.Sum(nil)

	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 4122 variant

	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
