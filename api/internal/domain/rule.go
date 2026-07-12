package domain

import (
	"context"
	"encoding/json"
	"time"
)

// RuleType enumerates the rule_definitions.type Postgres ENUM (the rule schema,
// the HTTP API contract §1.4). COUNTRY_RISK was added by migration 008 for the independent
// country risk table content (the rule schema §3.5).
type RuleType string

const (
	RuleTypeTMScenario      RuleType = "TM_SCENARIO"
	RuleTypeCDDWeight       RuleType = "CDD_WEIGHT"
	RuleTypeScreeningConfig RuleType = "SCREENING_CONFIG"
	RuleTypeCountryRisk     RuleType = "COUNTRY_RISK"
)

// RuleDefinition is a versioned rule_definitions row (TM scenario, CDD
// weight, screening config, or country risk table). Auditability First:
// updates never overwrite a version, they append a new row (see
// RuleRepository.CreateNewVersion).
type RuleDefinition struct {
	ID          string          `json:"id"`
	Type        RuleType        `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Definition  json.RawMessage `json:"definition"`
	Version     int             `json:"version"`
	IsActive    bool            `json:"is_active"`
	CreatedBy   string          `json:"created_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// RuleRepository manages versioned rule definitions. Updates never mutate an
// existing version row (Auditability First) — CreateNewVersion always
// inserts.
type RuleRepository interface {
	Get(ctx context.Context, id string) (*RuleDefinition, error)
	GetVersion(ctx context.Context, id string, version int) (*RuleDefinition, error)
	List(ctx context.Context, ruleType RuleType, activeOnly bool, limit int, after *Cursor) ([]RuleDefinition, error)
	// Create inserts a brand-new rule (version=1).
	Create(ctx context.Context, r *RuleDefinition) error
	// CreateNewVersion inserts a new row for an existing rule name with an
	// incremented version. It never UPDATEs an existing row.
	CreateNewVersion(ctx context.Context, r *RuleDefinition) error
	SetActive(ctx context.Context, id string, active bool) error
}
