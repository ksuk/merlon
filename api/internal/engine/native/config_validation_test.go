package native

import (
	"context"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/engine"
)

func validate(t *testing.T, typ, content string) *engine.ConfigValidationResult {
	t.Helper()
	// Validation is a pure function of the submitted document; it does not
	// read the engine's own configuration roots.
	e := &Engine{}
	result, err := e.ValidateConfig(context.Background(), typ, content)
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	return result
}

func onlyError(t *testing.T, result *engine.ConfigValidationResult) engine.ConfigValidationError {
	t.Helper()
	if result.Valid {
		t.Fatalf("expected an invalid result, got valid")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected exactly one error, got %d: %+v", len(result.Errors), result.Errors)
	}
	return result.Errors[0]
}

// TestValidateConfig_SyntaxErrorCarriesTheLine is the difference between an
// operator who can fix a paste and one who cannot: "parse error" alone does not
// say where to look.
func TestValidateConfig_SyntaxErrorCarriesTheLine(t *testing.T) {
	content := "risk_factors:\n  country:\n    weight: 1.0\n   values: {}\n"

	got := onlyError(t, validate(t, "cdd_weights", content))

	if got.Class != engine.ConfigErrorSyntax {
		t.Errorf("class = %q, want %q", got.Class, engine.ConfigErrorSyntax)
	}
	if got.Line <= 0 {
		t.Errorf("line = %d, want the position the parser rejected; message was %q", got.Line, got.Message)
	}
	if got.Severity != engine.ConfigSeverityError {
		t.Errorf("severity = %q, want %q", got.Severity, engine.ConfigSeverityError)
	}
}

// TestValidateConfig_SchemaErrorCarriesThePathAndPosition covers a document
// the parser accepts but the engine cannot reason about.
func TestValidateConfig_SchemaErrorCarriesThePathAndPosition(t *testing.T) {
	content := strings.Join([]string{
		"list_id: \"\"",
		"entries:",
		"  - id: e1",
		"    names: [Someone]",
	}, "\n")

	got := onlyError(t, validate(t, "screening_lists", content))

	if got.Class != engine.ConfigErrorSchema {
		t.Errorf("class = %q, want %q", got.Class, engine.ConfigErrorSchema)
	}
	if got.Path != "list_id" {
		t.Errorf("path = %q, want %q", got.Path, "list_id")
	}
	if got.Line != 1 {
		t.Errorf("line = %d, want 1 (the list_id key); message was %q", got.Line, got.Message)
	}
}

// TestValidateConfig_NestedSchemaPathResolvesToItsOwnLine proves the locator
// walks the document rather than matching the first key with that name.
func TestValidateConfig_NestedSchemaPathResolvesToItsOwnLine(t *testing.T) {
	content := strings.Join([]string{
		"list_id: sanctions",
		"entries:",
		"  - id: e1",
		"    names: []",
	}, "\n")

	got := onlyError(t, validate(t, "screening_lists", content))

	if got.Path != "entries[0].names" {
		t.Errorf("path = %q, want %q", got.Path, "entries[0].names")
	}
	if got.Line != 4 {
		t.Errorf("line = %d, want 4 (the names key inside the first entry)", got.Line)
	}
}

// TestValidateConfig_UnknownScenarioIsACrossReference distinguishes "this
// document is malformed" from "this document refers to something that does not
// exist here" — different mistakes with different fixes.
func TestValidateConfig_UnknownScenarioIsACrossReference(t *testing.T) {
	content := "scenario_id: no_such_scenario\nname: Test\nparameters:\n  threshold: 1\n"

	got := onlyError(t, validate(t, "tm_scenarios", content))

	if got.Class != engine.ConfigErrorCrossReference {
		t.Errorf("class = %q, want %q", got.Class, engine.ConfigErrorCrossReference)
	}
	if got.Path != "scenario_id" {
		t.Errorf("path = %q, want %q", got.Path, "scenario_id")
	}
	if got.Line != 1 {
		t.Errorf("line = %d, want 1", got.Line)
	}
}

// TestValidateConfig_UnknownConfigTypeHasNoPosition: the type is not part of
// the document, so claiming a line in it would be a fabricated location.
func TestValidateConfig_UnknownConfigTypeHasNoPosition(t *testing.T) {
	got := onlyError(t, validate(t, "scenario_rules", "id: x\n"))

	if got.Class != engine.ConfigErrorSchema {
		t.Errorf("class = %q, want %q", got.Class, engine.ConfigErrorSchema)
	}
	if got.Line != 0 || got.Column != 0 {
		t.Errorf("line/column = %d/%d, want 0/0: config_type is not a location in the document", got.Line, got.Column)
	}
}

// TestValidateConfig_ValidDocumentStaysValid guards against the classification
// work turning a good document into a rejected one.
func TestValidateConfig_ValidDocumentStaysValid(t *testing.T) {
	content := strings.Join([]string{
		"list_id: sanctions",
		"entries:",
		"  - id: e1",
		"    names: [Someone]",
	}, "\n")

	result := validate(t, "screening_lists", content)
	if !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("valid document rejected: %+v", result)
	}
}

// TestValidateConfig_WarningsDoNotBlock pins ADR-0025 DR-18: only errors stop a
// change. A warning that blocks becomes a warning everyone overrides, and the
// override stops meaning anything.
func TestValidateConfig_WarningsDoNotBlock(t *testing.T) {
	// A v2 scenario with no parameters is legal; the same document under the v1
	// shape is not. This one is well formed and merely sparse.
	content := "scenario_id: tm_structuring_basic\nname: Test\nschema_version: \"2.0\"\nconditions: {}\n"

	result := validate(t, "tm_scenarios", content)

	if !result.Valid {
		t.Fatalf("document rejected: %+v", result.Errors)
	}
	for _, w := range result.Warnings {
		if w.Severity != engine.ConfigSeverityWarning {
			t.Errorf("warning %q carries severity %q", w.Message, w.Severity)
		}
	}
}
