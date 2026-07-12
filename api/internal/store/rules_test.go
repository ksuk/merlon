package store

import (
	"context"
	"encoding/json"
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

	if err := repo.SetActive(ctx, "screening_defaults", true); err != nil {
		t.Fatalf("SetActive(true): %v", err)
	}
	got, err := repo.Get(ctx, "screening_defaults")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsActive {
		t.Error("expected IsActive=true after SetActive(true)")
	}

	if err := repo.SetActive(ctx, "screening_defaults", false); err != nil {
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
