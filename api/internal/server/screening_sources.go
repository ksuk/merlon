package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/screening"
)

const defaultScreeningSourceThreshold = 72 * time.Hour

func configuredScreeningSourceIDs(ids []string) []string {
	if len(ids) == 0 {
		return []string{"ofac_sdn", "eu_sanctions", "un_sc", "mof_japan", "pep_provider"}
	}
	return append([]string(nil), ids...)
}

// screeningSourceStatuses reads the same durable importer state used by the
// scheduled screening job.  The Wave3 repository remains a compatibility
// fallback for deployments that do not wire the importer (for example a
// read-only API process).
func (s *Server) screeningSourceStatuses(ctx context.Context, ids []string, threshold time.Duration) ([]domain.ScreeningSourceStatus, error) {
	ids = configuredScreeningSourceIDs(ids)
	if threshold <= 0 {
		threshold = defaultScreeningSourceThreshold
	}
	if s.screeningListStore != nil && s.screeningFailureTracker != nil {
		return readImporterSourceStatuses(ctx, ids, threshold, s.screeningListStore, s.screeningFailureTracker), nil
	}
	if workflow, ok := s.wave3.(domain.ScreeningWorkflowRepository); ok {
		items, err := workflow.ListScreeningSources(ctx, ids, threshold)
		if err == nil {
			return normalizeScreeningSourceStatuses(items, ids, threshold, "source directory returned incomplete data"), nil
		}
		// A source directory must not disappear when its status tracker is
		// temporarily unavailable. Returning one safe unavailable row per
		// configured source preserves cardinality and gives the UI an explicit
		// operational error state.
		return unavailableSourceStatuses(ids, threshold, "source status unavailable"), nil
	}
	return nil, fmt.Errorf("screening source directory not configured")
}

// screeningDegradation reports whether the required watchlist sources are
// usable right now. A run made while one is stale, failed or never imported is
// still executed -- blocking screening during a provider outage trades a
// missed detection for a halted operation -- but it is recorded as degraded so
// a clear result is not later mistaken for evidence of absence.
func (s *Server) screeningDegradation(ctx context.Context) domain.ScreeningDegradation {
	readiness := s.policies.ScreeningReadiness()
	if !readiness.MarksDegraded() {
		return domain.ScreeningDegradation{}
	}
	statuses, err := s.screeningSourceStatuses(ctx, nil, 0)
	if err != nil {
		// No source directory means no readiness signal at all, which is not
		// the same as a failing source. Marking every run degraded here would
		// leave the flag meaning nothing on the deployments that do report.
		slog.Warn("screening: source readiness could not be assessed; run recorded without a degradation claim", "error", err)
		return domain.ScreeningDegradation{}
	}
	var degraded []string
	for _, status := range statuses {
		if status.OperationalState == domain.ScreeningSourceReady {
			continue
		}
		if !readiness.Required(status.ListID) {
			continue
		}
		degraded = append(degraded, status.ListID)
	}
	if len(degraded) == 0 {
		return domain.ScreeningDegradation{}
	}
	return domain.ScreeningDegradation{Degraded: true, Sources: degraded}
}

func readImporterSourceStatuses(ctx context.Context, ids []string, threshold time.Duration, listStore screening.ListStore, tracker screening.FailureTracker) []domain.ScreeningSourceStatus {
	now := time.Now().UTC()
	out := make([]domain.ScreeningSourceStatus, 0, len(ids))
	reader, hasReader := tracker.(screening.FailureStatusReader)
	for _, id := range ids {
		status := domain.ScreeningSourceStatus{
			ListID: id, Configured: true,
			OperationalState:          domain.ScreeningSourceNeverImported,
			FreshnessThresholdSeconds: int64(threshold.Seconds()),
		}

		data, listErr := listStore.GetList(ctx, id)
		if data != nil {
			status.ListType = data.ListType
		}

		var snapshot screening.FailureStatus
		var trackerErr error
		if hasReader {
			snapshot, trackerErr = reader.FailureStatus(ctx, id)
		} else {
			var count int
			count, trackerErr = tracker.ConsecutiveFailures(ctx, id)
			snapshot.ConsecutiveFailures = count
			if trackerErr == nil {
				last, lastErr := tracker.LastSuccessAt(ctx, id)
				if lastErr == nil {
					snapshot.LastSuccessAt = sourceTimePtr(last)
				}
			}
		}
		if trackerErr != nil {
			status.OperationalState = domain.ScreeningSourceUnavailable
			status.Diagnostic = "source status unavailable"
			out = append(out, status)
			continue
		}
		status.LastAttemptAt = snapshot.LastAttemptAt
		status.LastFailureAt = snapshot.LastFailureAt
		status.LastSuccessAt = snapshot.LastSuccessAt
		status.ConsecutiveFailures = snapshot.ConsecutiveFailures
		status.Diagnostic = safeSourceDiagnostic(snapshot.Diagnostic)

		switch {
		case listErr != nil && !errors.Is(listErr, screening.ErrListNotFound):
			status.OperationalState = domain.ScreeningSourceUnreadable
			status.Diagnostic = "source snapshot unreadable"
		case listErr != nil && snapshot.LastSuccessAt != nil:
			// A success tracker row without its snapshot indicates corruption or
			// an incomplete restore; never report it as ready.
			status.OperationalState = domain.ScreeningSourceUnreadable
			status.Diagnostic = "last successful source snapshot unavailable"
		case snapshot.ConsecutiveFailures > 0:
			status.OperationalState = domain.ScreeningSourceFailed
			if status.Diagnostic == "" {
				status.Diagnostic = "source import failed"
			}
		case snapshot.LastSuccessAt == nil:
			status.OperationalState = domain.ScreeningSourceNeverImported
		case now.Sub(*snapshot.LastSuccessAt) > threshold:
			status.OperationalState = domain.ScreeningSourceStale
		default:
			status.OperationalState = domain.ScreeningSourceReady
		}
		if snapshot.LastSuccessAt != nil {
			age := int64(now.Sub(*snapshot.LastSuccessAt).Seconds())
			if age < 0 {
				age = 0
			}
			status.AgeSeconds = &age
		}
		out = append(out, status)
	}
	return out
}

func normalizeScreeningSourceStatuses(items []domain.ScreeningSourceStatus, ids []string, threshold time.Duration, diagnostic string) []domain.ScreeningSourceStatus {
	byID := make(map[string]domain.ScreeningSourceStatus, len(items))
	for _, item := range items {
		item.FreshnessThresholdSeconds = int64(threshold.Seconds())
		byID[item.ListID] = item
	}
	out := make([]domain.ScreeningSourceStatus, 0, len(ids))
	for _, id := range ids {
		item, ok := byID[id]
		if !ok {
			item = domain.ScreeningSourceStatus{ListID: id, Configured: true, OperationalState: domain.ScreeningSourceUnavailable, Diagnostic: diagnostic}
		}
		item.ListID = id
		item.Configured = true
		if item.FreshnessThresholdSeconds <= 0 {
			item.FreshnessThresholdSeconds = int64(threshold.Seconds())
		}
		out = append(out, item)
	}
	return out
}

func unavailableSourceStatuses(ids []string, threshold time.Duration, diagnostic string) []domain.ScreeningSourceStatus {
	out := make([]domain.ScreeningSourceStatus, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.ScreeningSourceStatus{
			ListID: id, Configured: true, OperationalState: domain.ScreeningSourceUnavailable,
			FreshnessThresholdSeconds: int64(threshold.Seconds()), Diagnostic: diagnostic,
		})
	}
	return out
}

func safeSourceDiagnostic(value string) string {
	// Current concrete trackers already redact diagnostics. Keep this guard at
	// the API boundary for custom trackers and legacy deployments.
	if value == "" {
		return ""
	}
	if len(value) > 200 {
		return "source status diagnostic unavailable"
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' {
			return "source status diagnostic unavailable"
		}
	}
	return value
}

func sourceTimePtr(value time.Time) *time.Time {
	copy := value
	return &copy
}
