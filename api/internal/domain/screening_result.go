package domain

import (
	"context"
	"fmt"
	"time"
)

// ScreeningResultStatus is the investigation-workflow status of a
// screening_results row (the screening workflow §スクリーニングヒット後の調査ワークフロー).
type ScreeningResultStatus string

const (
	ScreeningResultStatusNew           ScreeningResultStatus = "NEW"
	ScreeningResultStatusReviewing     ScreeningResultStatus = "REVIEWING"
	ScreeningResultStatusTruePositive  ScreeningResultStatus = "TRUE_POSITIVE"
	ScreeningResultStatusFalsePositive ScreeningResultStatus = "FALSE_POSITIVE"
)

// screeningResultTransitions enumerates the only allowed forward moves in the
// NEW -> REVIEWING -> {TRUE_POSITIVE, FALSE_POSITIVE} state machine
// (the screening workflow). Any transition not listed here, including regressions like
// FALSE_POSITIVE -> TRUE_POSITIVE, is rejected.
var screeningResultTransitions = map[ScreeningResultStatus]map[ScreeningResultStatus]bool{
	ScreeningResultStatusNew: {
		ScreeningResultStatusReviewing: true,
	},
	ScreeningResultStatusReviewing: {
		ScreeningResultStatusTruePositive:  true,
		ScreeningResultStatusFalsePositive: true,
	},
}

// IsValidScreeningResultTransition reports whether moving a screening_results
// row from `from` to `to` is permitted by the investigation workflow state
// machine.
func IsValidScreeningResultTransition(from, to ScreeningResultStatus) bool {
	return screeningResultTransitions[from][to]
}

// ScreeningResultRecord is a persisted screening hit (the screening workflow §7 data
// model / §スクリーニングヒット後の調査ワークフロー). Unlike ScreenMatch/ScreenResult
// (screening.go), which are transient API response shapes returned directly
// from the engine's single-shot screen call, this type is the durable
// investigation record stored in screening_results.
type ScreeningResultRecord struct {
	ID                  string                `json:"id"`
	CustomerID          string                `json:"customer_id"`
	ListID              string                `json:"list_id"`
	ListType            string                `json:"list_type"`
	EntryID             string                `json:"entry_id"`
	MatchedName         string                `json:"matched_name"`
	Similarity          float64               `json:"similarity"`
	Status              ScreeningResultStatus `json:"status"`
	FalsePositiveReason string                `json:"false_positive_reason,omitempty"`
	ReviewedBy          string                `json:"reviewed_by,omitempty"`
	ReviewedAt          *time.Time            `json:"reviewed_at,omitempty"`
	ScreenedAt          time.Time             `json:"screened_at"`
	CreatedAt           time.Time             `json:"created_at"`
}

// ApplyStatusTransition validates and applies a status change in place. For
// FALSE_POSITIVE, reason is mandatory (the screening workflow "判定理由（テキスト、必須）を記録").
// It never mutates r on an invalid transition.
func (r *ScreeningResultRecord) ApplyStatusTransition(to ScreeningResultStatus, falsePositiveReason string) error {
	if !IsValidScreeningResultTransition(r.Status, to) {
		return fmt.Errorf("invalid screening result status transition: %s -> %s", r.Status, to)
	}
	if to == ScreeningResultStatusFalsePositive && falsePositiveReason == "" {
		return fmt.Errorf("false_positive_reason is required when transitioning to FALSE_POSITIVE")
	}

	r.Status = to
	if to == ScreeningResultStatusFalsePositive {
		r.FalsePositiveReason = falsePositiveReason
	}
	return nil
}

// ScreeningResultRepository persists screening hits and supports lookup of
// past False Positive determinations for the same list entry (the screening workflow
// "同一リストエントリへの再ヒット時に過去の False Positive 判定を参照可能とする").
type ScreeningResultRepository interface {
	Get(ctx context.Context, id string) (*ScreeningResultRecord, error)
	Create(ctx context.Context, r *ScreeningResultRecord) error
	Update(ctx context.Context, r *ScreeningResultRecord) error
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]ScreeningResultRecord, error)
	ListByStatus(ctx context.Context, status ScreeningResultStatus, limit, offset int) ([]ScreeningResultRecord, error)
	ListPastFalsePositives(ctx context.Context, entryID string) ([]ScreeningResultRecord, error)
}
