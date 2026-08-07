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

// configuredScreeningSourceIDs resolves which sources a readiness view covers.
// The screening_readiness policy is the source of truth; an explicit request
// argument (a query parameter, or the deployment's own list) still wins so an
// operator can inspect a single feed.
func (s *Server) configuredScreeningSourceIDs(ids []string) []string {
	if len(ids) > 0 {
		return append([]string(nil), ids...)
	}
	return s.policies.ScreeningReadiness().SourceIDs()
}

// screeningSourceThresholds returns the per-source freshness window. A daily
// sanctions feed and a monthly PEP refresh are not stale at the same age, so
// the window is a function of the source rather than one global duration.
// override, when positive, is an explicit request-scoped window and applies to
// every source.
func (s *Server) screeningSourceThresholds(override time.Duration) func(string) time.Duration {
	if override > 0 {
		return func(string) time.Duration { return override }
	}
	readiness := s.policies.ScreeningReadiness()
	return readiness.ThresholdFor
}

// screeningSourceStatuses reads the same durable importer state used by the
// scheduled screening job.  The Wave3 repository remains a compatibility
// fallback for deployments that do not wire the importer (for example a
// read-only API process).
func (s *Server) screeningSourceStatuses(ctx context.Context, ids []string, override time.Duration) ([]domain.ScreeningSourceStatus, error) {
	ids = s.configuredScreeningSourceIDs(ids)
	thresholdFor := s.screeningSourceThresholds(override)
	if s.screeningListStore != nil && s.screeningFailureTracker != nil {
		return readImporterSourceStatuses(ctx, ids, thresholdFor, s.screeningListStore, s.screeningFailureTracker), nil
	}
	if workflow, ok := s.wave3.(domain.ScreeningWorkflowRepository); ok {
		items, err := workflow.ListScreeningSources(ctx, ids, thresholdFor)
		if err == nil {
			return normalizeScreeningSourceStatuses(items, ids, thresholdFor, "source directory returned incomplete data"), nil
		}
		// A source directory must not disappear when its status tracker is
		// temporarily unavailable. Returning one safe unavailable row per
		// configured source preserves cardinality and gives the UI an explicit
		// operational error state.
		return unavailableSourceStatuses(ids, thresholdFor, "source status unavailable"), nil
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
	degraded := unreadyRequiredSources(statuses, readiness.Required)
	if len(degraded) == 0 {
		return domain.ScreeningDegradation{}
	}
	return domain.ScreeningDegradation{Degraded: true, Sources: degraded}
}

func readImporterSourceStatuses(ctx context.Context, ids []string, thresholdFor func(string) time.Duration, listStore screening.ListStore, tracker screening.FailureTracker) []domain.ScreeningSourceStatus {
	now := time.Now().UTC()
	out := make([]domain.ScreeningSourceStatus, 0, len(ids))
	reader, hasReader := tracker.(screening.FailureStatusReader)
	for _, id := range ids {
		snapshot := domain.ScreeningSourceSnapshot{ListID: id}

		data, listErr := listStore.GetList(ctx, id)
		if data != nil {
			snapshot.ListType = data.ListType
		}
		snapshot.SnapshotUnreadable = listErr != nil && !errors.Is(listErr, screening.ErrListNotFound)
		snapshot.SnapshotMissing = listErr != nil

		var status screening.FailureStatus
		var trackerErr error
		if hasReader {
			status, trackerErr = reader.FailureStatus(ctx, id)
		} else {
			var count int
			count, trackerErr = tracker.ConsecutiveFailures(ctx, id)
			status.ConsecutiveFailures = count
			if trackerErr == nil {
				last, lastErr := tracker.LastSuccessAt(ctx, id)
				if lastErr == nil {
					status.LastSuccessAt = sourceTimePtr(last)
				}
			}
		}
		if trackerErr != nil {
			// Nothing below the tracker is trustworthy, so report exactly that
			// rather than a freshness verdict derived from unread state.
			snapshot = domain.ScreeningSourceSnapshot{ListID: id, ListType: snapshot.ListType, StatusUnavailable: true}
		} else {
			snapshot.LastAttemptAt = status.LastAttemptAt
			snapshot.LastFailureAt = status.LastFailureAt
			snapshot.LastSuccessAt = status.LastSuccessAt
			snapshot.ConsecutiveFailures = status.ConsecutiveFailures
			snapshot.Diagnostic = safeSourceDiagnostic(status.Diagnostic)
		}
		out = append(out, domain.ClassifyScreeningSource(snapshot, thresholdFor(id), now))
	}
	return out
}

func normalizeScreeningSourceStatuses(items []domain.ScreeningSourceStatus, ids []string, thresholdFor func(string) time.Duration, diagnostic string) []domain.ScreeningSourceStatus {
	byID := make(map[string]domain.ScreeningSourceStatus, len(items))
	for _, item := range items {
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
			item.FreshnessThresholdSeconds = int64(thresholdFor(id).Seconds())
		}
		out = append(out, item)
	}
	return out
}

func unavailableSourceStatuses(ids []string, thresholdFor func(string) time.Duration, diagnostic string) []domain.ScreeningSourceStatus {
	out := make([]domain.ScreeningSourceStatus, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.ScreeningSourceStatus{
			ListID: id, Configured: true, OperationalState: domain.ScreeningSourceUnavailable,
			FreshnessThresholdSeconds: int64(thresholdFor(id).Seconds()), Diagnostic: diagnostic,
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
