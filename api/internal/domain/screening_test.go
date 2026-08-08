package domain

import (
	"encoding/json"
	"testing"
)

func TestScreenResultJSONNormalizesEmptyMatches(t *testing.T) {
	data, err := json.Marshal(ScreenResult{CustomerID: "customer-1", Hit: false})
	if err != nil {
		t.Fatalf("marshal screen result: %v", err)
	}

	var got struct {
		Matches []ScreenMatch `json:"matches"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal screen result: %v", err)
	}
	if got.Matches == nil {
		t.Fatalf("matches = null in %s, want []", data)
	}
}
