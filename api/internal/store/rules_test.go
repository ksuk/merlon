package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func newTestRule(name string, ruleType domain.RuleType, active bool, at time.Time) *domain.RuleDefinition {
	return &domain.RuleDefinition{
		ID:         generateTestID(int(at.Unix())),
		Type:       ruleType,
		Name:       name,
		Definition: json.RawMessage(`{"schema_version":"1.0"}`),
		Version:    1,
		IsActive:   active,
		CreatedBy:  "maker",
		CreatedAt:  at,
		UpdatedAt:  at,
	}
}

func TestRuleRepo_CreateAndGet(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	rule := newTestRule("structuring_detection", domain.RuleTypeTMScenario, true, base)
	if err := repo.Create(ctx, rule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, rule.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.Name != rule.Name || got.Type != rule.Type {
		t.Errorf("got %+v, want name=%s type=%s", got, rule.Name, rule.Type)
	}
}

func TestRuleRepo_CreateNewVersion_DoesNotOverwrite(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	v1 := newTestRule("cdd_basic_weights", domain.RuleTypeCDDWeight, true, base)
	v1.Definition = json.RawMessage(`{"schema_version":"1.0","note":"v1"}`)
	if err := repo.Create(ctx, v1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	v2 := newTestRule("cdd_basic_weights", domain.RuleTypeCDDWeight, true, base.Add(time.Minute))
	v2.Definition = json.RawMessage(`{"schema_version":"1.0","note":"v2"}`)
	if err := repo.CreateNewVersion(ctx, v2); err != nil {
		t.Fatalf("CreateNewVersion (v2): %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("v2.Version = %d, want 2", v2.Version)
	}

	v3 := newTestRule("cdd_basic_weights", domain.RuleTypeCDDWeight, true, base.Add(2*time.Minute))
	v3.Definition = json.RawMessage(`{"schema_version":"1.0","note":"v3"}`)
	if err := repo.CreateNewVersion(ctx, v3); err != nil {
		t.Fatalf("CreateNewVersion (v3): %v", err)
	}
	if v3.Version != 3 {
		t.Fatalf("v3.Version = %d, want 3", v3.Version)
	}

	got1, err := repo.GetVersion(ctx, "cdd_basic_weights", 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	got2, err := repo.GetVersion(ctx, "cdd_basic_weights", 2)
	if err != nil {
		t.Fatalf("GetVersion(2): %v", err)
	}
	if string(got1.Definition) == string(got2.Definition) {
		t.Errorf("version 1 and 2 should have distinct content, both = %s", got1.Definition)
	}
	if string(got1.Definition) != `{"schema_version":"1.0","note":"v1"}` {
		t.Errorf("version 1 content = %s, want v1 note preserved", got1.Definition)
	}
	if string(got2.Definition) != `{"schema_version":"1.0","note":"v2"}` {
		t.Errorf("version 2 content = %s, want v2 note preserved", got2.Definition)
	}
}

func TestRuleRepo_Get_ReturnsLatestVersionByDefault(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	v1 := newTestRule("screening_defaults", domain.RuleTypeScreeningConfig, true, base)
	if err := repo.Create(ctx, v1); err != nil {
		t.Fatalf("Create: %v", err)
	}
	v2 := newTestRule("screening_defaults", domain.RuleTypeScreeningConfig, true, base.Add(time.Minute))
	if err := repo.CreateNewVersion(ctx, v2); err != nil {
		t.Fatalf("CreateNewVersion: %v", err)
	}

	got, err := repo.Get(ctx, "screening_defaults")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("Get returned version %d, want latest (2)", got.Version)
	}
}

func TestRuleRepo_List_FiltersByTypeAndActive(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	setup := func(t *testing.T) *MemoryRuleRepo {
		t.Helper()
		repo := NewMemoryRuleRepo()
		ctx := context.Background()
		must := func(r *domain.RuleDefinition) {
			t.Helper()
			if err := repo.Create(ctx, r); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}
		must(newTestRule("tm_active", domain.RuleTypeTMScenario, true, base))
		must(newTestRule("tm_inactive", domain.RuleTypeTMScenario, false, base.Add(time.Minute)))
		must(newTestRule("cdd_active", domain.RuleTypeCDDWeight, true, base.Add(2*time.Minute)))
		must(newTestRule("country_active", domain.RuleTypeCountryRisk, true, base.Add(3*time.Minute)))
		return repo
	}

	cases := []struct {
		name       string
		ruleType   domain.RuleType
		activeOnly bool
		wantNames  []string
	}{
		{"all types, all active states", "", false, []string{"country_active", "cdd_active", "tm_inactive", "tm_active"}},
		{"all types, active only", "", true, []string{"country_active", "cdd_active", "tm_active"}},
		{"tm scenario only", domain.RuleTypeTMScenario, false, []string{"tm_inactive", "tm_active"}},
		{"tm scenario active only", domain.RuleTypeTMScenario, true, []string{"tm_active"}},
		{"country risk only", domain.RuleTypeCountryRisk, false, []string{"country_active"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := setup(t)
			got, err := repo.List(context.Background(), tc.ruleType, tc.activeOnly, 50, nil)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != len(tc.wantNames) {
				t.Fatalf("List returned %d rules, want %d (%+v)", len(got), len(tc.wantNames), got)
			}
			for i, want := range tc.wantNames {
				if got[i].Name != want {
					t.Errorf("index %d: got name %s, want %s", i, got[i].Name, want)
				}
			}
		})
	}
}

func TestRuleRepo_SetActive_TogglesFlag(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	rule := newTestRule("screening_defaults", domain.RuleTypeScreeningConfig, false, base)
	if err := repo.Create(ctx, rule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := repo.SetActive(ctx, "screening_defaults", true, "checker"); err != nil {
		t.Fatalf("SetActive(true): %v", err)
	}
	got, err := repo.Get(ctx, "screening_defaults")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsActive {
		t.Error("expected IsActive=true after SetActive(true)")
	}

	if _, err := repo.SetActive(ctx, "screening_defaults", false, "checker"); err != nil {
		t.Fatalf("SetActive(false): %v", err)
	}
	got, err = repo.Get(ctx, "screening_defaults")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IsActive {
		t.Error("expected IsActive=false after SetActive(false)")
	}
}

func TestRuleRepo_SetActive_OnlyOneVersionActivePerName(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	v1 := newTestRule("cdd_basic_weights", domain.RuleTypeCDDWeight, true, base)
	if err := repo.Create(ctx, v1); err != nil {
		t.Fatalf("Create: %v", err)
	}
	v2 := newTestRule("cdd_basic_weights", domain.RuleTypeCDDWeight, true, base.Add(time.Minute))
	if err := repo.CreateNewVersion(ctx, v2); err != nil {
		t.Fatalf("CreateNewVersion: %v", err)
	}

	oldVersion, err := repo.GetVersion(ctx, "cdd_basic_weights", 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if oldVersion.IsActive {
		t.Error("creating v2 as active should deactivate v1 (only one active version per name)")
	}
}

func TestRuleRepo_SetActiveFalse_DeactivatesEveryVersion(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	now := time.Now()
	if err := repo.Create(ctx, newTestRule("all_versions", domain.RuleTypeTMScenario, true, now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateNewVersion(ctx, newTestRule("all_versions", domain.RuleTypeTMScenario, true, now.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetActive(ctx, "all_versions", false, "checker"); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 2; version++ {
		rule, err := repo.GetVersion(ctx, "all_versions", version)
		if err != nil {
			t.Fatal(err)
		}
		if rule.IsActive {
			t.Errorf("version %d remained active", version)
		}
	}
}

func TestRuleRepo_SetActiveChecksAuthorOfTargetVersion(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	v1 := newTestRule("dual_control", domain.RuleTypeCDDWeight, true, base)
	v1.CreatedBy = "admin-a"
	if err := repo.Create(ctx, v1); err != nil {
		t.Fatal(err)
	}
	v2 := newTestRule("dual_control", domain.RuleTypeCDDWeight, false, base.Add(time.Minute))
	v2.CreatedBy = "admin-b"
	if err := repo.CreateNewVersion(ctx, v2); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.SetActive(ctx, "dual_control", true, "admin-b"); err == nil {
		t.Fatal("author of latest version activated their own rule; want separation-of-duties error")
	} else {
		var sod *domain.ErrSeparationOfDuties
		if !errors.As(err, &sod) {
			t.Fatalf("error = %T %v, want *domain.ErrSeparationOfDuties", err, err)
		}
		if sod.Version != 2 {
			t.Errorf("error version = %d, want 2", sod.Version)
		}
	}
	active, err := repo.GetActive(ctx, "dual_control")
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != 1 {
		t.Fatalf("active version after denied approval = %d, want 1", active.Version)
	}

	change, err := repo.SetActive(ctx, "dual_control", true, "admin-c")
	if err != nil {
		t.Fatal(err)
	}
	if change.TargetVersion != 2 || change.TargetCreatedBy != "admin-b" || !change.Changed {
		t.Fatalf("change = %+v, want v2 authored by admin-b and changed", change)
	}
	active, err = repo.GetActive(ctx, "dual_control")
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != 2 {
		t.Fatalf("active version = %d, want 2", active.Version)
	}
}

func TestRuleRepo_SetActiveFailsClosedWithoutCreator(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	rule := newTestRule("legacy", domain.RuleTypeTMScenario, false, time.Now())
	rule.CreatedBy = ""
	if err := repo.Create(ctx, rule); err != nil {
		t.Fatal(err)
	}

	_, err := repo.SetActive(ctx, "legacy", true, "checker")
	var sod *domain.ErrSeparationOfDuties
	if !errors.As(err, &sod) {
		t.Fatalf("error = %T %v, want *domain.ErrSeparationOfDuties", err, err)
	}
}

func TestPgRuleRepo_SetActiveAtomicallyRecordsIndependentApproval(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPostgresRuleRepo(pool)
	ctx := context.Background()
	name := "dual-control-" + newTestUUID()
	now := time.Now().UTC()

	v1 := &domain.RuleDefinition{
		ID: newTestUUID(), Type: domain.RuleTypeCDDWeight, Name: name,
		Definition: json.RawMessage(`{"schema_version":"1.0"}`), IsActive: true,
		CreatedBy: "admin-a", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(ctx, v1); err != nil {
		t.Fatal(err)
	}
	v2 := &domain.RuleDefinition{
		ID: newTestUUID(), Type: domain.RuleTypeCDDWeight, Name: name,
		Definition: json.RawMessage(`{"schema_version":"1.0"}`), IsActive: false,
		CreatedBy: "admin-b", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	if err := repo.CreateNewVersion(ctx, v2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM rule_activation_events WHERE rule_name = $1`, name)
		pool.Exec(context.Background(), `DELETE FROM rule_definitions WHERE name = $1`, name)
	})

	_, err := repo.SetActive(ctx, name, true, "admin-b")
	var sod *domain.ErrSeparationOfDuties
	if !errors.As(err, &sod) {
		t.Fatalf("self approval error = %T %v, want *domain.ErrSeparationOfDuties", err, err)
	}
	active, err := repo.GetActive(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != 1 {
		t.Fatalf("active version after denied approval = %d, want 1", active.Version)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM rule_activation_events WHERE rule_name = $1`, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("approval events after denied change = %d, want 0", count)
	}

	change, err := repo.SetActive(ctx, name, true, "admin-c")
	if err != nil {
		t.Fatal(err)
	}
	if change.TargetVersion != 2 || !change.Changed || change.Current.Version != 2 || !change.Current.IsActive {
		t.Fatalf("change = %+v, want active latest version 2", change)
	}
	var version int
	var author, approver string
	var requestedActive, changed bool
	if err := pool.QueryRow(ctx, `SELECT rule_version, rule_created_by, approved_by, requested_active, changed
		FROM rule_activation_events WHERE rule_name = $1`, name).
		Scan(&version, &author, &approver, &requestedActive, &changed); err != nil {
		t.Fatal(err)
	}
	if version != 2 || author != "admin-b" || approver != "admin-c" || !requestedActive || !changed {
		t.Fatalf("approval event = v%d %q %q active=%t changed=%t", version, author, approver, requestedActive, changed)
	}
}

func TestPgRuleRepo_ConcurrentVersionCreationCannotSelfApprove(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPostgresRuleRepo(pool)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		name := "dual-control-race-" + newTestUUID()
		now := time.Now().UTC()
		v1 := &domain.RuleDefinition{
			ID: newTestUUID(), Type: domain.RuleTypeCDDWeight, Name: name,
			Definition: json.RawMessage(`{"schema_version":"1.0"}`), IsActive: true,
			CreatedBy: "admin-a", CreatedAt: now, UpdatedAt: now,
		}
		if err := repo.Create(ctx, v1); err != nil {
			t.Fatal(err)
		}
		v2 := &domain.RuleDefinition{
			ID: newTestUUID(), Type: domain.RuleTypeCDDWeight, Name: name,
			Definition: json.RawMessage(`{"schema_version":"1.0"}`), IsActive: false,
			CreatedBy: "admin-b", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var createErr, activateErr error
		go func() {
			defer wg.Done()
			<-start
			createErr = repo.CreateNewVersion(ctx, v2)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, activateErr = repo.SetActive(ctx, name, true, "admin-b")
		}()
		close(start)
		wg.Wait()

		if createErr != nil {
			t.Fatalf("iteration %d create: %v", i, createErr)
		}
		if activateErr != nil {
			var sod *domain.ErrSeparationOfDuties
			if !errors.As(activateErr, &sod) {
				t.Fatalf("iteration %d activate: %T %v", i, activateErr, activateErr)
			}
		}
		active, err := repo.GetActive(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if active.Version != 1 {
			t.Fatalf("iteration %d self-authored version became active: %+v", i, active)
		}
		pool.Exec(ctx, `DELETE FROM rule_activation_events WHERE rule_name = $1`, name)
		pool.Exec(ctx, `DELETE FROM rule_definitions WHERE name = $1`, name)
	}
}

func TestRuleRepo_List_CursorPagination(t *testing.T) {
	repo := NewMemoryRuleRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	const n = 5
	names := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		r := newTestRule(generateTestID(i), domain.RuleTypeTMScenario, true, base.Add(time.Duration(i)*time.Minute))
		names[r.Name] = true
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	seen := map[string]bool{}
	var after *domain.Cursor
	for {
		page, err := repo.List(ctx, "", false, 3, after)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		trimmed, hasMore := page, false
		if len(page) > 2 {
			trimmed, hasMore = page[:2], true
		}
		for _, r := range trimmed {
			if seen[r.Name] {
				t.Errorf("duplicate rule in cursor traversal: %s", r.Name)
			}
			seen[r.Name] = true
		}
		if !hasMore {
			break
		}
		last := trimmed[len(trimmed)-1]
		after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	if len(seen) != n {
		t.Errorf("cursor traversal found %d rules, want %d", len(seen), n)
	}
	for name := range names {
		if !seen[name] {
			t.Errorf("cursor traversal missing rule %s", name)
		}
	}
}
