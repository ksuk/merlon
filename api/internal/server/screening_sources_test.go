package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/screening"
	"github.com/ksuk/merlon/api/internal/store"
)

type sourceStatusTracker struct {
	status map[string]screening.FailureStatus
	err    error
}

func (t *sourceStatusTracker) RecordSuccess(context.Context, string) error        { return nil }
func (t *sourceStatusTracker) RecordFailure(context.Context, string) (int, error) { return 0, nil }
func (t *sourceStatusTracker) ConsecutiveFailures(context.Context, string) (int, error) {
	return 0, nil
}
func (t *sourceStatusTracker) LastSuccessAt(context.Context, string) (time.Time, error) {
	return time.Time{}, errors.New("not used")
}
func (t *sourceStatusTracker) FailureStatus(_ context.Context, id string) (screening.FailureStatus, error) {
	if t.err != nil {
		return screening.FailureStatus{}, t.err
	}
	return t.status[id], nil
}

type sourceListStore struct {
	data map[string]*screening.RawListData
	errs map[string]error
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func (s *sourceListStore) SaveList(_ context.Context, data *screening.RawListData) error {
	s.data[data.ListID] = data
	return nil
}
func (s *sourceListStore) GetList(_ context.Context, id string) (*screening.RawListData, error) {
	if err := s.errs[id]; err != nil {
		return nil, err
	}
	data, ok := s.data[id]
	if !ok {
		return nil, screening.ErrListNotFound
	}
	return data, nil
}

func TestScreeningSourcesPreserveConfiguredCardinalityAndClassifyStates(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-25 * time.Hour)
	listStore := &sourceListStore{
		data: map[string]*screening.RawListData{
			"ready":  {ListID: "ready", ListType: "sanctions"},
			"stale":  {ListID: "stale", ListType: "pep"},
			"failed": {ListID: "failed", ListType: "sanctions"},
		},
		errs: map[string]error{"unreadable": errors.New("decode failed")},
	}
	tracker := &sourceStatusTracker{status: map[string]screening.FailureStatus{
		"ready":  {LastAttemptAt: timePtr(now), LastSuccessAt: timePtr(now)},
		"stale":  {LastAttemptAt: timePtr(old), LastSuccessAt: timePtr(old)},
		"failed": {LastAttemptAt: timePtr(now), LastFailureAt: timePtr(now), ConsecutiveFailures: 3, Diagnostic: "safe diagnostic"},
	}}
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(), ScreeningListStore: listStore, ScreeningFailureTracker: tracker, ScreeningListIDs: []string{"ready", "stale", "failed", "unreadable", "never"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/screening/sources?freshness_threshold_seconds=86400", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data            []domain.ScreeningSourceStatus `json:"data"`
		ConfiguredCount int                            `json:"configured_count"`
	}
	decodeJSON(t, rec, &body)
	if body.ConfiguredCount != 5 || len(body.Data) != 5 {
		t.Fatalf("source directory = %+v, want five configured rows", body)
	}
	states := make(map[string]domain.ScreeningSourceState, len(body.Data))
	for _, item := range body.Data {
		states[item.ListID] = item.OperationalState
	}
	want := map[string]domain.ScreeningSourceState{
		"ready": domain.ScreeningSourceReady, "stale": domain.ScreeningSourceStale,
		"failed": domain.ScreeningSourceFailed, "unreadable": domain.ScreeningSourceUnreadable,
		"never": domain.ScreeningSourceNeverImported,
	}
	for id, expected := range want {
		if states[id] != expected {
			t.Errorf("%s state = %q, want %q", id, states[id], expected)
		}
	}
}

func TestScreeningSourcesExposeUnavailableOnTrackerFailure(t *testing.T) {
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(),
		ScreeningListStore:      screening.NewMemoryListStore(),
		ScreeningFailureTracker: &sourceStatusTracker{err: errors.New("tracker down")},
		ScreeningListIDs:        []string{"ofac_sdn", "mof_japan"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/screening/sources", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []domain.ScreeningSourceStatus `json:"data"`
	}
	decodeJSON(t, rec, &body)
	if len(body.Data) != 2 {
		t.Fatalf("data length = %d, want 2", len(body.Data))
	}
	for _, item := range body.Data {
		if item.OperationalState != domain.ScreeningSourceUnavailable || item.Diagnostic == "" {
			t.Errorf("unavailable source = %+v", item)
		}
	}
}

func TestScreeningSourcesRejectMalformedFreshnessThreshold(t *testing.T) {
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: store.NewMemoryWave3Repo()})
	for _, raw := range []string{"0", "-1", "abc", "1e5", "99999999999999999999", " ", "1.5"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/screening/sources?freshness_threshold_seconds="+url.QueryEscape(raw), nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("freshness_threshold_seconds=%q = %d, want 400", raw, rec.Code)
		}
	}
	// An absent parameter falls back to the policy window rather than failing.
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/screening/sources", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("absent threshold = %d, want 200", rec.Code)
	}
}

func TestScreeningSourcesRejectEmptySourceID(t *testing.T) {
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: store.NewMemoryWave3Repo()})
	for _, raw := range []string{"ofac_sdn,", ",", "ofac_sdn, ,un_sc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/screening/sources?source_ids="+url.QueryEscape(raw), nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("source_ids=%q = %d, want 400: an empty identifier would silently widen the directory", raw, rec.Code)
		}
	}
}

// Without a source_ids argument the directory comes from the
// screening_readiness policy, not from a list hardcoded in Go.
func TestScreeningSourcesDefaultToThePolicyDirectory(t *testing.T) {
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: store.NewMemoryWave3Repo()})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/screening/sources", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data            []domain.ScreeningSourceStatus `json:"data"`
		ConfiguredCount int                            `json:"configured_count"`
		ScreeningReady  bool                           `json:"screening_ready"`
		PolicyVersion   string                         `json:"policy_version"`
	}
	decodeJSON(t, rec, &body)
	want := policy.DefaultScreeningReadiness().SourceIDs()
	if body.ConfiguredCount != len(want) || len(body.Data) != len(want) {
		t.Fatalf("configured_count = %d, want the policy's %d sources", body.ConfiguredCount, len(want))
	}
	for i, id := range want {
		if body.Data[i].ListID != id {
			t.Fatalf("source %d = %q, want %q in policy order", i, body.Data[i].ListID, id)
		}
	}
	if body.PolicyVersion != policy.DefaultScreeningReadiness().Version() {
		t.Errorf("policy_version = %q, want the readiness policy's version", body.PolicyVersion)
	}
	// Nothing has been imported, so every required source is unready.
	if body.ScreeningReady {
		t.Error("screening_ready = true with no source ever imported")
	}
}

// An optional source being unready must not make the deployment unready:
// screening_ready answers only for the sources the policy requires.
func TestScreeningSourcesReadyIgnoresOptionalSources(t *testing.T) {
	now := time.Now().UTC()
	readiness := policy.DefaultScreeningReadiness()
	listStore := &sourceListStore{data: map[string]*screening.RawListData{}, errs: map[string]error{}}
	tracker := &sourceStatusTracker{status: map[string]screening.FailureStatus{}}
	for _, id := range readiness.SourceIDs() {
		if readiness.Required(id) {
			listStore.data[id] = &screening.RawListData{ListID: id, ListType: "sanctions"}
			tracker.status[id] = screening.FailureStatus{LastAttemptAt: timePtr(now), LastSuccessAt: timePtr(now)}
		}
	}
	s := New(":0", Deps{
		Customers:          store.NewMemoryCustomerRepo(),
		ScreeningListStore: listStore, ScreeningFailureTracker: tracker,
	})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/screening/sources", nil))
	var body struct {
		ScreeningReady  bool     `json:"screening_ready"`
		DegradedSources []string `json:"degraded_sources"`
		UnreadyCount    int      `json:"unready_count"`
	}
	decodeJSON(t, rec, &body)
	if !body.ScreeningReady || len(body.DegradedSources) != 0 {
		t.Fatalf("body = %+v, want ready: only the optional PEP feed is missing", body)
	}
	if body.UnreadyCount == 0 {
		t.Error("unready_count = 0; the optional source is still reported, just not decisive")
	}
}
