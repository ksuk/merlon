package domain

import (
	"encoding/json"
	"testing"
)

// TestCounterpartyJSONRoundTrip verifies Counterparty (the data model
// §1.3.1 travel-rule originator/beneficiary data) marshals and unmarshals
// back to an identical value, matching how PgTransactionRepo persists it in
// the transactions.counterparty JSONB column.
func TestCounterpartyJSONRoundTrip(t *testing.T) {
	original := Counterparty{
		CounterpartyType: CounterpartyTypeVASP,
		Originator: CounterpartyParty{
			Name:          "Taro Yamada",
			AccountNumber: "1234567890",
		},
		Beneficiary: CounterpartyParty{
			Name:          "Jane Doe",
			AccountNumber: "0987654321",
			VASPName:      "Example VASP Inc.",
		},
		TravelRuleStatus: TravelRuleComplete,
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Counterparty
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded != original {
		t.Errorf("round trip mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestCounterpartyJSONRoundTripIncompleteTravelRule(t *testing.T) {
	original := Counterparty{
		CounterpartyType: CounterpartyTypeUnhostedWallet,
		Originator:       CounterpartyParty{Name: "Taro Yamada"},
		Beneficiary:      CounterpartyParty{AccountNumber: "unknown-wallet-address"},
		TravelRuleStatus: TravelRuleIncomplete,
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Counterparty
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != original {
		t.Errorf("round trip mismatch: got %+v, want %+v", decoded, original)
	}
}
