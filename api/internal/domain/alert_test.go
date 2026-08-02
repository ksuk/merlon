package domain

import "testing"

func TestValidAlertStatusTransition(t *testing.T) {
	tests := []struct {
		name string
		from AlertStatus
		to   AlertStatus
		want bool
	}{
		{"open to investigating", AlertStatusOpen, AlertStatusInvestigating, true},
		{"open to escalated", AlertStatusOpen, AlertStatusEscalated, true},
		{"open to terminal is held for DR-02", AlertStatusOpen, AlertStatusClosedFalsePositive, false},
		{"investigating to escalated", AlertStatusInvestigating, AlertStatusEscalated, true},
		{"investigating to true positive", AlertStatusInvestigating, AlertStatusClosedTruePositive, true},
		{"escalated to investigating", AlertStatusEscalated, AlertStatusInvestigating, true},
		{"escalated to false positive", AlertStatusEscalated, AlertStatusClosedFalsePositive, true},
		{"terminal is immutable", AlertStatusClosedTruePositive, AlertStatusInvestigating, false},
		{"suppressed is system-only", AlertStatusSuppressed, AlertStatusOpen, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidAlertStatusTransition(tt.from, tt.to); got != tt.want {
				t.Fatalf("ValidAlertStatusTransition(%q, %q) = %t, want %t", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestAlertQueueLifecycleSets(t *testing.T) {
	for _, status := range []AlertStatus{AlertStatusOpen, AlertStatusInvestigating, AlertStatusEscalated} {
		if !IsAlertUnresolved(status) {
			t.Errorf("IsAlertUnresolved(%q) = false", status)
		}
	}
	for _, status := range []AlertStatus{AlertStatusClosedTruePositive, AlertStatusClosedFalsePositive} {
		if !IsAlertTerminal(status) {
			t.Errorf("IsAlertTerminal(%q) = false", status)
		}
		if IsAlertUnresolved(status) {
			t.Errorf("IsAlertUnresolved(%q) = true", status)
		}
	}
}
