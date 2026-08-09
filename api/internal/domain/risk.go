package domain

// AlertSeverityRank returns the explicit AML queue rank for an alert
// severity. Unknown values sort after every known risk level and remain
// visible to the caller rather than being silently promoted.
func AlertSeverityRank(severity AlertSeverity) int {
	switch severity {
	case AlertSeverityCritical:
		return 4
	case AlertSeverityHigh:
		return 3
	case AlertSeverityMedium:
		return 2
	case AlertSeverityLow:
		return 1
	default:
		return 0
	}
}

// CasePriorityRank uses the same explicit risk order as alert severity.
func CasePriorityRank(priority CasePriority) int {
	switch priority {
	case CasePriorityCritical:
		return 4
	case CasePriorityHigh:
		return 3
	case CasePriorityMedium:
		return 2
	case CasePriorityLow:
		return 1
	default:
		return 0
	}
}
