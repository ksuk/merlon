package screening

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fakeAdapter is a test ListAdapter that either returns fixed data or a
// fixed error, so importer tests can control fetch outcomes without any
// real network access.
type fakeAdapter struct {
	data *RawListData
	err  error
}

func (a *fakeAdapter) FetchList(_ context.Context) (*RawListData, error) {
	if a.err != nil {
		return nil, a.err
	}
	return a.data, nil
}

func rawList(listID string, entryIDs ...string) *RawListData {
	data := &RawListData{ListID: listID, ListType: "sanctions", Name: listID, Source: "test"}
	for _, id := range entryIDs {
		data.Entries = append(data.Entries, RawListEntry{EntryID: id, Names: []string{id}})
	}
	return data
}

func TestRunImportJob_FullReplaceOnSuccess(t *testing.T) {
	store := NewMemoryListStore()
	tracker := NewMemoryFailureTracker()
	ctx := context.Background()

	// Seed the store with a prior version of the list to prove the new
	// fetch fully replaces it rather than merging.
	if err := store.SaveList(ctx, rawList("ofac_sdn", "OLD-1", "OLD-2")); err != nil {
		t.Fatalf("seed SaveList: %v", err)
	}

	adapters := map[string]ListAdapter{
		"ofac_sdn": &fakeAdapter{data: rawList("ofac_sdn", "NEW-1")},
	}

	result, err := RunImportJob(ctx, adapters, store, tracker)
	if err != nil {
		t.Fatalf("RunImportJob: %v", err)
	}
	if len(result.Outcomes) != 1 || !result.Outcomes[0].Imported {
		t.Fatalf("outcomes = %+v, want single imported outcome", result.Outcomes)
	}

	got, err := store.GetList(ctx, "ofac_sdn")
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0].EntryID != "NEW-1" {
		t.Errorf("stored list = %+v, want full replace with only NEW-1", got.Entries)
	}
}

func TestRunImportJob_ContinuesWithPreviousListOnFailure(t *testing.T) {
	store := NewMemoryListStore()
	tracker := NewMemoryFailureTracker()
	ctx := context.Background()

	if err := store.SaveList(ctx, rawList("eu_sanctions", "EU-1")); err != nil {
		t.Fatalf("seed SaveList: %v", err)
	}

	adapters := map[string]ListAdapter{
		"eu_sanctions": &fakeAdapter{err: errors.New("upstream unavailable")},
	}

	result, err := RunImportJob(ctx, adapters, store, tracker)
	if err != nil {
		t.Fatalf("RunImportJob: %v", err)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].Imported || result.Outcomes[0].Err == nil {
		t.Fatalf("outcomes = %+v, want single failed outcome", result.Outcomes)
	}

	got, err := store.GetList(ctx, "eu_sanctions")
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0].EntryID != "EU-1" {
		t.Errorf("stored list = %+v, want previous list preserved on failure", got.Entries)
	}
}

func TestRunImportJob_TracksConsecutiveFailures(t *testing.T) {
	store := NewMemoryListStore()
	tracker := NewMemoryFailureTracker()
	ctx := context.Background()

	adapters := map[string]ListAdapter{
		"un_sc": &fakeAdapter{err: errors.New("timeout")},
	}

	// Day 1 and 2: below threshold, no operational alert.
	for i := 1; i <= 2; i++ {
		result, err := RunImportJob(ctx, adapters, store, tracker)
		if err != nil {
			t.Fatalf("RunImportJob day %d: %v", i, err)
		}
		outcome := result.Outcomes[0]
		if outcome.ConsecutiveFailures != i {
			t.Errorf("day %d: ConsecutiveFailures = %d, want %d", i, outcome.ConsecutiveFailures, i)
		}
		if outcome.NeedsOperationalAlert {
			t.Errorf("day %d: NeedsOperationalAlert = true, want false (threshold is 3)", i)
		}
	}

	// Day 3: consecutive failures reach the default 3-day threshold
	// (the screening workflow "連続 N 日間（デフォルト：3 日）取得失敗した場合、運用アラート").
	result, err := RunImportJob(ctx, adapters, store, tracker)
	if err != nil {
		t.Fatalf("RunImportJob day 3: %v", err)
	}
	outcome := result.Outcomes[0]
	if outcome.ConsecutiveFailures != 3 {
		t.Errorf("day 3: ConsecutiveFailures = %d, want 3", outcome.ConsecutiveFailures)
	}
	if !outcome.NeedsOperationalAlert {
		t.Error("day 3: NeedsOperationalAlert = false, want true at the 3-day threshold")
	}
}

func TestRunImportJob_ResetsFailureCountOnSuccess(t *testing.T) {
	store := NewMemoryListStore()
	tracker := NewMemoryFailureTracker()
	ctx := context.Background()

	failing := map[string]ListAdapter{"mof_japan": &fakeAdapter{err: errors.New("503")}}
	for i := 0; i < 2; i++ {
		if _, err := RunImportJob(ctx, failing, store, tracker); err != nil {
			t.Fatalf("RunImportJob failing iteration %d: %v", i, err)
		}
	}
	if n, _ := tracker.ConsecutiveFailures(ctx, "mof_japan"); n != 2 {
		t.Fatalf("ConsecutiveFailures = %d, want 2 before recovery", n)
	}

	recovering := map[string]ListAdapter{"mof_japan": &fakeAdapter{data: rawList("mof_japan", "MOF-1")}}
	result, err := RunImportJob(ctx, recovering, store, tracker)
	if err != nil {
		t.Fatalf("RunImportJob recovery: %v", err)
	}
	if !result.Outcomes[0].Imported {
		t.Fatalf("outcomes = %+v, want imported on recovery", result.Outcomes)
	}

	n, err := tracker.ConsecutiveFailures(ctx, "mof_japan")
	if err != nil {
		t.Fatalf("ConsecutiveFailures: %v", err)
	}
	if n != 0 {
		t.Errorf("ConsecutiveFailures after success = %d, want 0 (reset)", n)
	}
}

func TestRunImportJob_PEPNotConfiguredIsSkippedNotFailed(t *testing.T) {
	store := NewMemoryListStore()
	tracker := NewMemoryFailureTracker()
	ctx := context.Background()

	adapters := map[string]ListAdapter{
		"pep_provider": &PEPAdapter{ListID: "pep_provider", URL: ""},
	}

	result, err := RunImportJob(ctx, adapters, store, tracker)
	if err != nil {
		t.Fatalf("RunImportJob: %v", err)
	}
	if len(result.Outcomes) != 1 {
		t.Fatalf("outcomes = %+v, want 1", result.Outcomes)
	}
	outcome := result.Outcomes[0]
	if !outcome.Skipped || outcome.Imported {
		t.Errorf("outcome = %+v, want Skipped=true, Imported=false", outcome)
	}

	// Skipping must not count as a failure (the screening workflow "PEP リスト未設定の場合、
	// PEP 照合はスキップされるが、その旨を監査ログに記録する" — not an operational
	// failure requiring the fail-alert consecutive-failure counter).
	n, err := tracker.ConsecutiveFailures(ctx, "pep_provider")
	if err != nil {
		t.Fatalf("ConsecutiveFailures: %v", err)
	}
	if n != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0 for a skip", n)
	}
}

type getListErrorStore struct {
	ListStore
	errors map[string]error
}

func (s *getListErrorStore) GetList(ctx context.Context, listID string) (*RawListData, error) {
	if err := s.errors[listID]; err != nil {
		return nil, err
	}
	return s.ListStore.GetList(ctx, listID)
}

type recordingListConsumer struct {
	calls int
	lists []RawListData
}

func (c *recordingListConsumer) ReplaceScreeningLists(lists []RawListData) {
	c.calls++
	c.lists = append([]RawListData(nil), lists...)
}

func runImportOnceWithConsumer(t *testing.T, adapters map[string]ListAdapter, store ListStore, tracker FailureTracker, consumer ListConsumer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	RunImportJobPeriodicallyWithConsumer(ctx, time.Hour, adapters, store, tracker, consumer)
}

func TestRunImportJobPeriodicallyWithConsumer_GetListErrorRetainsEntirePreviousSnapshot(t *testing.T) {
	memoryStore := NewMemoryListStore()
	if err := memoryStore.SaveList(context.Background(), rawList("ofac_sdn", "OLD-OFAC")); err != nil {
		t.Fatalf("seed OFAC list: %v", err)
	}
	if err := memoryStore.SaveList(context.Background(), rawList("un_sc", "OLD-UN")); err != nil {
		t.Fatalf("seed UN list: %v", err)
	}
	store := &getListErrorStore{
		ListStore: memoryStore,
		errors:    map[string]error{"un_sc": errors.New("database temporarily unavailable")},
	}
	consumer := &recordingListConsumer{
		lists: []RawListData{*rawList("ofac_sdn", "ACTIVE-OFAC"), *rawList("un_sc", "ACTIVE-UN")},
	}
	adapters := map[string]ListAdapter{
		"ofac_sdn": &fakeAdapter{data: rawList("ofac_sdn", "NEW-OFAC")},
		"un_sc":    &fakeAdapter{err: errors.New("upstream unavailable")},
	}

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	runImportOnceWithConsumer(t, adapters, store, NewMemoryFailureTracker(), consumer)

	if consumer.calls != 0 {
		t.Fatalf("ReplaceScreeningLists calls = %d, want 0 so the entire active snapshot is retained", consumer.calls)
	}
	if len(consumer.lists) != 2 || consumer.lists[0].Entries[0].EntryID != "ACTIVE-OFAC" || consumer.lists[1].Entries[0].EntryID != "ACTIVE-UN" {
		t.Fatalf("consumer snapshot = %+v, want the complete previous snapshot", consumer.lists)
	}
	if got := logs.String(); !strings.Contains(got, `"needs_operational_alert":true`) {
		t.Errorf("logs = %q, want needs_operational_alert=true", got)
	}
}

func TestRunImportJobPeriodicallyWithConsumer_FirstUnconfiguredPEPNotFoundIsSkipped(t *testing.T) {
	store := NewMemoryListStore()
	consumer := &recordingListConsumer{}
	adapters := map[string]ListAdapter{
		"ofac_sdn":     &fakeAdapter{data: rawList("ofac_sdn", "OFAC-1")},
		"pep_provider": &PEPAdapter{ListID: "pep_provider", URL: ""},
	}

	runImportOnceWithConsumer(t, adapters, store, NewMemoryFailureTracker(), consumer)

	if consumer.calls != 1 {
		t.Fatalf("ReplaceScreeningLists calls = %d, want 1", consumer.calls)
	}
	if len(consumer.lists) != 1 || consumer.lists[0].ListID != "ofac_sdn" {
		t.Fatalf("consumer lists = %+v, want only ofac_sdn", consumer.lists)
	}
}

func TestRunImportJobPeriodicallyWithConsumer_UnconfiguredPEPRetainsPreviousSnapshot(t *testing.T) {
	store := NewMemoryListStore()
	if err := store.SaveList(context.Background(), rawList("pep_provider", "PEP-1")); err != nil {
		t.Fatalf("seed PEP list: %v", err)
	}
	consumer := &recordingListConsumer{}
	adapters := map[string]ListAdapter{
		"ofac_sdn":     &fakeAdapter{data: rawList("ofac_sdn", "OFAC-1")},
		"pep_provider": &PEPAdapter{ListID: "pep_provider", URL: ""},
	}

	runImportOnceWithConsumer(t, adapters, store, NewMemoryFailureTracker(), consumer)

	if consumer.calls != 1 {
		t.Fatalf("ReplaceScreeningLists calls = %d, want 1", consumer.calls)
	}
	if len(consumer.lists) != 2 || consumer.lists[0].ListID != "ofac_sdn" || consumer.lists[1].ListID != "pep_provider" {
		t.Fatalf("consumer lists = %+v, want OFAC and previous PEP snapshots", consumer.lists)
	}
	if got := consumer.lists[1].Entries[0].EntryID; got != "PEP-1" {
		t.Errorf("PEP entry = %q, want retained PEP-1", got)
	}
}

func TestRunImportJobPeriodically_RunsImmediatelyAndStopsOnCancel(t *testing.T) {
	store := NewMemoryListStore()
	tracker := NewMemoryFailureTracker()
	adapters := map[string]ListAdapter{"ofac_sdn": &fakeAdapter{data: rawList("ofac_sdn", "X-1")}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		RunImportJobPeriodically(ctx, time.Millisecond, adapters, store, tracker)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunImportJobPeriodically did not return after context cancellation")
	}

	if _, err := store.GetList(context.Background(), "ofac_sdn"); err != nil {
		t.Errorf("expected list to be imported at least once immediately, GetList error: %v", err)
	}
}
