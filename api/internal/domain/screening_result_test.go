package domain

import "testing"

func TestScreeningResultStatus_ValidTransitions(t *testing.T) {
	cases := []struct {
		name    string
		from    ScreeningResultStatus
		to      ScreeningResultStatus
		allowed bool
	}{
		{"new_to_reviewing", ScreeningResultStatusNew, ScreeningResultStatusReviewing, true},
		{"reviewing_to_true_positive", ScreeningResultStatusReviewing, ScreeningResultStatusTruePositive, true},
		{"reviewing_to_false_positive", ScreeningResultStatusReviewing, ScreeningResultStatusFalsePositive, true},
		{"new_to_true_positive_direct_rejected", ScreeningResultStatusNew, ScreeningResultStatusTruePositive, false},
		{"new_to_false_positive_direct_rejected", ScreeningResultStatusNew, ScreeningResultStatusFalsePositive, false},
		{"false_positive_to_true_positive_rejected", ScreeningResultStatusFalsePositive, ScreeningResultStatusTruePositive, false},
		{"true_positive_to_false_positive_rejected", ScreeningResultStatusTruePositive, ScreeningResultStatusFalsePositive, false},
		{"true_positive_to_reviewing_rejected", ScreeningResultStatusTruePositive, ScreeningResultStatusReviewing, false},
		{"reviewing_to_new_rejected", ScreeningResultStatusReviewing, ScreeningResultStatusNew, false},
		{"same_status_rejected", ScreeningResultStatusNew, ScreeningResultStatusNew, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsValidScreeningResultTransition(tc.from, tc.to)
			if got != tc.allowed {
				t.Errorf("IsValidScreeningResultTransition(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.allowed)
			}
		})
	}
}

func TestScreeningResultStatus_FalsePositiveRequiresReason(t *testing.T) {
	r := ScreeningResultRecord{Status: ScreeningResultStatusReviewing}
	if err := r.ApplyStatusTransition(ScreeningResultStatusFalsePositive, ""); err == nil {
		t.Error("expected error when false_positive_reason is empty")
	}
	if err := r.ApplyStatusTransition(ScreeningResultStatusFalsePositive, "confirmed different person"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if r.Status != ScreeningResultStatusFalsePositive {
		t.Errorf("status = %q, want FALSE_POSITIVE", r.Status)
	}
	if r.FalsePositiveReason != "confirmed different person" {
		t.Errorf("false_positive_reason = %q", r.FalsePositiveReason)
	}
}

func TestScreeningResultStatus_ApplyInvalidTransitionRejected(t *testing.T) {
	r := ScreeningResultRecord{Status: ScreeningResultStatusNew}
	if err := r.ApplyStatusTransition(ScreeningResultStatusTruePositive, ""); err == nil {
		t.Error("expected error for NEW -> TRUE_POSITIVE direct transition")
	}
	if r.Status != ScreeningResultStatusNew {
		t.Errorf("status must remain unchanged after rejected transition, got %q", r.Status)
	}
}
