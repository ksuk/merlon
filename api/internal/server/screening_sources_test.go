package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
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
