package domain

import "time"

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
}
