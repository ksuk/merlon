package native

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

func screeningSecondaryFixture() *Engine {
	return &Engine{
		screeningThreshold: 0.85,
		screeningDigest:    "snapshot-test-digest",
		lists: []screeningList{{
			ID: "sanctions", Type: "sanctions", Source: "fixture",
			Entries: []screeningEntry{
				{ID: "match", Names: []string{"Example Person"}, DatesOfBirth: []string{"1980-01-02", "1981-02-03"}, Addresses: []string{"1-2-3 Chiyoda Tokyo"}, Country: "JP", EntityType: "individual"},
				{ID: "mismatch", Names: []string{"Example Person"}, DatesOfBirth: []string{"1970-01-01"}, Addresses: []string{"999 Other Street Osaka"}, Country: "KP", EntityType: "entity"},
			},
		}},
	}
}

func TestScreenCustomerSecondaryIdentifiersAdjustConfidenceWithoutDiscardingNameCandidates(t *testing.T) {
	e := screeningSecondaryFixture()
	customer := &domain.Customer{ID: "c1", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Attributes: map[string]any{
		"name": "Example Person", "date_of_birth": "1980-01-02", "address": "1-2-3 Chiyoda Tokyo",
	}}
	result, err := e.ScreenCustomer(context.Background(), customer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("matches=%d, want both name candidates retained", len(result.Matches))
	}
	if result.Matches[0].EntryID != "match" || result.Matches[0].Confidence != 1 {
		t.Fatalf("best match=%+v, want matching secondary identifiers and clamped confidence", result.Matches[0])
	}
	if result.Matches[1].EntryID != "mismatch" || result.Matches[1].Confidence != 0.8 {
		t.Fatalf("mismatch=%+v, want name score minus secondary penalties", result.Matches[1])
	}
	statuses, ok := result.Matches[1].MatchEvidence["secondary_identifiers"].(map[string]string)
	if !ok || statuses["date_of_birth"] != "mismatch" || statuses["address"] != "mismatch" || statuses["country"] != "mismatch" || statuses["entity_type"] != "mismatch" {
		t.Fatalf("secondary evidence=%v", result.Matches[1].MatchEvidence)
	}
}

func TestScreenCustomerMissingIdentifiersDoNotChangeNameConfidence(t *testing.T) {
	e := screeningSecondaryFixture()
	result, err := e.ScreenCustomer(context.Background(), &domain.Customer{ID: "c1", Attributes: map[string]any{"name": "Example Person"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("matches=%d, want all name candidates", len(result.Matches))
	}
	for _, match := range result.Matches {
		if match.Confidence != 1 {
			t.Errorf("%s confidence=%v, want unchanged name score", match.EntryID, match.Confidence)
		}
	}
}

func TestScreenCustomerEvidenceContainsOnlyVersionedFingerprints(t *testing.T) {
	e := screeningSecondaryFixture()
	secretDOB := "1980-01-02"
	secretAddress := "1-2-3 Chiyoda Tokyo"
	result, err := e.ScreenCustomer(context.Background(), &domain.Customer{ID: "c1", Attributes: map[string]any{
		"name": "Example Person", "date_of_birth": secretDOB, "address": secretAddress,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(result.Matches[0].MatchEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secretDOB) || strings.Contains(string(b), secretAddress) {
		t.Fatalf("evidence leaked secondary identifier plaintext: %s", b)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"policy_version", "policy_digest", "algorithm_version", "list_snapshot_digest", "fingerprints"} {
		if decoded[key] == nil {
			t.Errorf("evidence missing %q: %s", key, b)
		}
	}
	fingerprints, ok := decoded["fingerprints"].(map[string]any)
	if !ok || fingerprints["date_of_birth"] == nil {
		t.Fatalf("fingerprints=%v", decoded["fingerprints"])
	}
	if strings.Contains(string(b), "19800102") {
		t.Fatalf("evidence leaked normalized DOB: %s", b)
	}
}

func TestAddressTokenSimilarityUsesContractBoundaries(t *testing.T) {
	if got := addressTokenSimilarity("1-2-3 Chiyoda Tokyo", "1 2 3 Chiyoda Tokyo"); got < 0.80 {
		t.Fatalf("exact normalized address similarity=%v", got)
	}
	if got := addressTokenSimilarity("1 Chiyoda Tokyo", "999 Other Osaka"); got >= 0.50 {
		t.Fatalf("mismatching address similarity=%v", got)
	}
}
