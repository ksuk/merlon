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

func TestCaseAlertStateCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		caseStatus  CaseStatus
		alertStatus AlertStatus
		want        bool
	}{
		{"active case with active alert", CaseStatusInvestigating, AlertStatusOpen, true},
		{"reopened case keeps terminal history", CaseStatusReopened, AlertStatusClosedFalsePositive, true},
		{"closed case with terminal alert", CaseStatusClosed, AlertStatusClosedFalsePositive, true},
		{"str filed case with true positive", CaseStatusStrFiled, AlertStatusClosedTruePositive, true},
		{"closed case rejects active alert", CaseStatusClosed, AlertStatusInvestigating, false},
		{"str filed case rejects active alert", CaseStatusStrFiled, AlertStatusEscalated, false},
		{"unknown case state fails closed", CaseStatus("future"), AlertStatusOpen, false},
		{"unknown alert state fails closed", CaseStatusNew, AlertStatus("future"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompatibleCaseAlertState(tt.caseStatus, tt.alertStatus); got != tt.want {
				t.Errorf("CompatibleCaseAlertState(%q, %q) = %t, want %t", tt.caseStatus, tt.alertStatus, got, tt.want)
			}
		})
	}
}

func TestCanAttachAlertToCase(t *testing.T) {
	if !CanAttachAlertToCase(CaseStatusReopened, AlertStatusOpen) {
		t.Fatal("reopened case must accept a new unresolved alert")
	}
	if CanAttachAlertToCase(CaseStatusClosed, AlertStatusOpen) {
		t.Fatal("terminal case accepted a new alert")
	}
	if CanAttachAlertToCase(CaseStatusNew, AlertStatusClosedTruePositive) {
		t.Fatal("new case accepted a terminal alert as a new link")
	}
}
