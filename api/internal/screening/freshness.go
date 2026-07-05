package screening

import (
	"time"

	"github.com/merlon-aml/merlon/api/internal/metrics"
)

// ListImportStatus is one configured list's last-known successful-import
// timestamp, the input ComputeListFreshness needs to derive staleness
// (screening.md "リストの鮮度情報（最終更新日時）をダッシュボードに表示する").
type ListImportStatus struct {
	ListID        string
	ListType      string
	LastSuccessAt time.Time
}

// ListFreshness is one list's computed freshness: days since its last
// successful import, and whether that has crossed the default 3-day
// operational-alert threshold (screening.md "連続 N 日間（デフォルト：3 日）取得
// 失敗した場合、運用アラート...を発行").
type ListFreshness struct {
	ListID                string
	ListType              string
	StaleDays             int
	NeedsOperationalAlert bool
}

// ComputeListFreshness computes each list's current staleness relative to
// now.
func ComputeListFreshness(lists []ListImportStatus) []ListFreshness {
	now := time.Now()
	out := make([]ListFreshness, 0, len(lists))
	for _, l := range lists {
		staleDays := 0
		if !l.LastSuccessAt.IsZero() {
			staleDays = int(now.Sub(l.LastSuccessAt).Hours() / 24)
		}
		out = append(out, ListFreshness{
			ListID:                l.ListID,
			ListType:              l.ListType,
			StaleDays:             staleDays,
			NeedsOperationalAlert: staleDays >= staleFailureThreshold,
		})
	}
	return out
}

// RecordListFreshnessMetrics publishes merlon_screening_list_stale_days
// (overview.md §4.4 OPS-003, label: list_type). Several lists share one
// list_type (OFAC/EU/UN/MOF are all "sanctions"); this records the worst
// (maximum) staleness among them per type, so a stale list is never masked
// by a fresher one sharing its type (Fail-Alert principle: err toward
// alerting).
func RecordListFreshnessMetrics(freshnesses []ListFreshness) {
	worst := make(map[string]int, len(freshnesses))
	for _, f := range freshnesses {
		if cur, ok := worst[f.ListType]; !ok || f.StaleDays > cur {
			worst[f.ListType] = f.StaleDays
		}
	}
	for listType, days := range worst {
		metrics.ScreeningListStaleDays.WithLabelValues(listType).Set(float64(days))
	}
}
