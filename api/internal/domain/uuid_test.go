package domain

import "testing"

func TestNormalizeUUIDUsesCompactPublicRepresentation(t *testing.T) {
	for _, input := range []string{
		"44444444-4444-4444-8444-444444444444",
		"44444444444444448444444444444444",
	} {
		got, err := NormalizeUUID(input)
		if err != nil {
			t.Fatalf("NormalizeUUID(%q): %v", input, err)
		}
		if got != "44444444444444448444444444444444" {
			t.Fatalf("NormalizeUUID(%q) = %q", input, got)
		}
	}
}

func TestNormalizeUUIDRejectsMalformedValues(t *testing.T) {
	for _, input := range []string{"", "not-a-uuid", "44444444-4444-4444-4444-44444444444z", "4444-4444-4444-4444-444444444444"} {
		if _, err := NormalizeUUID(input); err == nil {
			t.Errorf("NormalizeUUID(%q) accepted malformed UUID", input)
		}
	}
}

func TestCanonicalIdentifierPreservesNonUUIDTextIDs(t *testing.T) {
	if got := CanonicalIdentifier(" case-001 "); got != "case-001" {
		t.Fatalf("CanonicalIdentifier(non-UUID) = %q", got)
	}
	if !SameIdentifier("44444444444444448444444444444444", "44444444-4444-4444-8444-444444444444") {
		t.Fatal("UUID representations were not compared canonically")
	}
}
