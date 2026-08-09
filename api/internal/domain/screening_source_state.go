package domain

import "time"

// ScreeningSourceSnapshot is the raw, unjudged state of one configured
// watchlist source: what the importer last did and whether the imported body
// can still be read. Every backing store reports these same facts; deciding
// what they mean is ClassifyScreeningSource's job alone.
//
// The three implementations that previously each made that decision -- the
// importer reader, the in-memory repository and the PostgreSQL repository --
// disagreed. The memory store never produced unreadable at all, so a
// deployment could see a source reported ready by one path and unreadable by
// another for the same underlying state.
type ScreeningSourceSnapshot struct {
	ListID              string
	ListType            string
	LastAttemptAt       *time.Time
	LastSuccessAt       *time.Time
	LastFailureAt       *time.Time
	ConsecutiveFailures int
	Diagnostic          string

	// StatusUnavailable means the import tracker itself could not be read, so
	// nothing below it is trustworthy.
	StatusUnavailable bool
	// SnapshotUnreadable means the stored list body exists but could not be
	// read (corruption, a permissions fault, an interrupted restore).
	SnapshotUnreadable bool
	// SnapshotMissing means no imported list body is present. Combined with a
	// recorded successful import it indicates an incomplete restore.
	SnapshotMissing bool
}

// ClassifyScreeningSource turns a snapshot into the operational state the API
// and UI act on. The order of the checks is the contract: a source whose
// status cannot be read is never reported as merely stale, and a source whose
// body is unreadable is never reported ready no matter how recent its last
// successful import claims to be.
func ClassifyScreeningSource(snapshot ScreeningSourceSnapshot, threshold time.Duration, now time.Time) ScreeningSourceStatus {
	status := ScreeningSourceStatus{
		ListID:                    snapshot.ListID,
		ListType:                  snapshot.ListType,
		Configured:                true,
		LastAttemptAt:             snapshot.LastAttemptAt,
		LastFailureAt:             snapshot.LastFailureAt,
		LastSuccessAt:             snapshot.LastSuccessAt,
		ConsecutiveFailures:       snapshot.ConsecutiveFailures,
		Diagnostic:                snapshot.Diagnostic,
		FreshnessThresholdSeconds: int64(threshold.Seconds()),
	}
	if snapshot.LastSuccessAt != nil {
		age := int64(now.Sub(*snapshot.LastSuccessAt).Seconds())
		if age < 0 {
			// A clock skew between the importer and the API must not present
			// as a source that is fresher than possible.
			age = 0
		}
		status.AgeSeconds = &age
	}

	switch {
	case snapshot.StatusUnavailable:
		status.OperationalState = ScreeningSourceUnavailable
		if status.Diagnostic == "" {
			status.Diagnostic = "source status unavailable"
		}
	case snapshot.SnapshotUnreadable:
		status.OperationalState = ScreeningSourceUnreadable
		status.Diagnostic = "source snapshot unreadable"
	case snapshot.SnapshotMissing && snapshot.LastSuccessAt != nil:
		// The tracker records a successful import whose body is gone. That is
		// corruption or an incomplete restore, not freshness.
		status.OperationalState = ScreeningSourceUnreadable
		status.Diagnostic = "last successful source snapshot unavailable"
	case snapshot.ConsecutiveFailures > 0:
		status.OperationalState = ScreeningSourceFailed
		if status.Diagnostic == "" {
			status.Diagnostic = "source import failed"
		}
	case snapshot.LastSuccessAt == nil:
		status.OperationalState = ScreeningSourceNeverImported
	case now.Sub(*snapshot.LastSuccessAt) > threshold:
		status.OperationalState = ScreeningSourceStale
	default:
		status.OperationalState = ScreeningSourceReady
	}
	return status
}

// IsReady reports whether a source is usable for screening. Only one of the
// six states qualifies, which is why callers must not test for "not failed".
func (s ScreeningSourceStatus) IsReady() bool {
	return s.OperationalState == ScreeningSourceReady
}
