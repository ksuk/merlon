package domain

import (
	"encoding/json"
	"time"
)

type ScreenMatch struct {
	ListID      string  `json:"list_id"`
	EntryID     string  `json:"entry_id"`
	MatchedName string  `json:"matched_name"`
	Similarity  float64 `json:"similarity"`
	ListType    string  `json:"list_type"`
	Source      string  `json:"source"`
}

type ScreenResult struct {
	CustomerID   string        `json:"customer_id"`
	Hit          bool          `json:"hit"`
	Matches      []ScreenMatch `json:"matches"`
	ListsChecked int           `json:"lists_checked"`
	ScreenedAt   time.Time     `json:"screened_at"`
	RunID        string        `json:"run_id,omitempty"`
	ResultIDs    []string      `json:"result_ids,omitempty"`
}

// MarshalJSON keeps collection fields stable for API consumers. A nil slice
// is a valid Go representation of no matches, but it serializes as JSON null
// and violates the collection contract used by the UI.
func (r ScreenResult) MarshalJSON() ([]byte, error) {
	type screenResult ScreenResult
	normalized := screenResult(r)
	if normalized.Matches == nil {
		normalized.Matches = []ScreenMatch{}
	}
	return json.Marshal(normalized)
}
