package domain

import "testing"

// TestValidCaseStatusTransition_AllowsSpecTransitions verifies every edge in
// the case-management workflow's status transition diagram (NEW -> INVESTIGATING ->
// ESCALATED/CLOSED/STR_FILED, with ESCALATED->INVESTIGATING and
// CLOSED->REOPENED->INVESTIGATING as the only backward edges).
func TestValidCaseStatusTransition_AllowsSpecTransitions(t *testing.T) {
	tests := []struct {
		name string
		from CaseStatus
		to   CaseStatus
	}{
		{"new to investigating", CaseStatusNew, CaseStatusInvestigating},
		{"investigating to escalated", CaseStatusInvestigating, CaseStatusEscalated},
		{"escalated back to investigating", CaseStatusEscalated, CaseStatusInvestigating},
		{"investigating to closed", CaseStatusInvestigating, CaseStatusClosed},
		{"closed to reopened", CaseStatusClosed, CaseStatusReopened},
		{"reopened to investigating", CaseStatusReopened, CaseStatusInvestigating},
		{"investigating to str_filed", CaseStatusInvestigating, CaseStatusStrFiled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !ValidCaseStatusTransition(tt.from, tt.to) {
				t.Errorf("ValidCaseStatusTransition(%q, %q) = false, want true", tt.from, tt.to)
			}
		})
	}
}

// TestValidCaseStatusTransition_RejectsInvalidTransitions checks that
// terminal states cannot transition further and that skip-level jumps are
// rejected. str_filed is terminal for the existing case; a new alert on the
// same customer becomes a separate case with a reference link instead
// (the case-management workflow "STR_FILED は終端状態").
func TestValidCaseStatusTransition_RejectsInvalidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from CaseStatus
		to   CaseStatus
	}{
		{"str_filed is terminal", CaseStatusStrFiled, CaseStatusInvestigating},
		{"closed cannot go directly to investigating", CaseStatusClosed, CaseStatusInvestigating},
		{"new cannot skip to escalated", CaseStatusNew, CaseStatusEscalated},
		{"new cannot skip to closed", CaseStatusNew, CaseStatusClosed},
		{"reopened cannot go to closed directly", CaseStatusReopened, CaseStatusClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ValidCaseStatusTransition(tt.from, tt.to) {
				t.Errorf("ValidCaseStatusTransition(%q, %q) = true, want false", tt.from, tt.to)
			}
		})
	}
}

// TestValidCaseStatusTransition_OpenTreatedAsNewAlias verifies the 12-month
// Contract Stability alias: existing rows with status="open" behave exactly
// like "new".
func TestValidCaseStatusTransition_OpenTreatedAsNewAlias(t *testing.T) {
	if !ValidCaseStatusTransition(CaseStatusOpen, CaseStatusInvestigating) {
		t.Error("ValidCaseStatusTransition(open, investigating) = false, want true (open is a new alias)")
	}
}
