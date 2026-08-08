package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// Every shipped default must validate. A default that cannot pass its own
// Validate() would mean an unconfigured deployment runs under rules the
// loader would have rejected from a file.
func TestDefaultPoliciesValidate(t *testing.T) {
	if err := DefaultKYCRequiredFields().Validate(); err != nil {
		t.Fatalf("kyc default: %v", err)
	}
	if err := DefaultEDD().Validate(); err != nil {
		t.Fatalf("edd default: %v", err)
	}
	if err := DefaultCDDRuleSelection().Validate(); err != nil {
		t.Fatalf("cdd rule selection default: %v", err)
	}
	if err := DefaultTravelRule().Validate(); err != nil {
		t.Fatalf("travel rule default: %v", err)
	}
	if err := DefaultScreeningReadiness().Validate(); err != nil {
		t.Fatalf("screening readiness default: %v", err)
	}
}

// A nil *Set must behave exactly like the defaults. Handler tests build a
// Server without policies, and they must keep working unchanged.
func TestNilSetReturnsDefaults(t *testing.T) {
	var set *Set
	if set.KYC().Enforce() != KYCEnforcementWarn {
		t.Fatal("nil set must yield warn enforcement")
	}
	if days, ok := set.EDD().StageDays("stage3"); !ok || days != 90 {
		t.Fatalf("nil set stage3 = %d, %v; want 90, true", days, ok)
	}
	if set.TravelRule().ThresholdAmount != 100000 {
		t.Fatalf("nil set threshold = %v; want 100000", set.TravelRule().ThresholdAmount)
	}
	if !set.ScreeningReadiness().Required("ofac_sdn") {
		t.Fatal("nil set must treat ofac_sdn as required")
	}
	if !set.CDDRuleSelection().ServerResolves() {
		t.Fatal("nil set must resolve rule sets server-side")
	}
	if got := len(set.Descriptors()); got != len(Names()) {
		t.Fatalf("nil set descriptors = %d; want %d", got, len(Names()))
	}
}

// A blank path is the documented way to ask for the in-code default.
func TestLoadBlankPathYieldsDefault(t *testing.T) {
	set, err := Load(Paths{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, descriptor := range set.Descriptors() {
		if descriptor.Source != "default" {
			t.Fatalf("%s source = %q; want default", descriptor.Name, descriptor.Source)
		}
		if descriptor.PolicyVersion == "" || descriptor.Digest == "" {
			t.Fatalf("%s must report a version and digest", descriptor.Name)
		}
	}
}

// Digests must be stable for identical content and differ for different
// content, or an auditor cannot use them to prove which rules applied.
func TestDigestIsContentStable(t *testing.T) {
	first := digest(DefaultTravelRule())
	second := digest(DefaultTravelRule())
	if first != second {
		t.Fatal("digest is not stable for identical documents")
	}
	changed := DefaultTravelRule()
	changed.ThresholdAmount = 50000
	if digest(changed) == first {
		t.Fatal("digest did not change when the threshold changed")
	}
}

func writePolicyFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestKYCRequiredFieldsRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "wrong schema version",
			body: "schema_version: kyc_required_fields_v2\npolicy_version: x\nenforcement: warn\ndefaults:\n  required: [name]\n",
			want: "schema_version",
		},
		{
			name: "missing policy version",
			body: "schema_version: kyc_required_fields_v1\npolicy_version: \"\"\nenforcement: warn\ndefaults:\n  required: [name]\n",
			want: "policy_version",
		},
		{
			name: "unknown enforcement",
			body: "schema_version: kyc_required_fields_v1\npolicy_version: x\nenforcement: block\ndefaults:\n  required: [name]\n",
			want: "enforcement",
		},
		{
			name: "unknown customer type",
			body: "schema_version: kyc_required_fields_v1\npolicy_version: x\nenforcement: warn\ndefaults:\n  required: [name]\ntypes:\n  martian:\n    required: [name]\n",
			want: "not a known customer type",
		},
		{
			name: "field required and recommended",
			body: "schema_version: kyc_required_fields_v1\npolicy_version: x\nenforcement: warn\ndefaults:\n  required: [name]\n  recommended: [name]\n",
			want: "both required and recommended",
		},
		{
			// A type that resolves to nothing is the exact defect the policy
			// exists to close: corporate_foreign silently accepting a record
			// with no identity at all.
			name: "type resolves to no required fields",
			body: "schema_version: kyc_required_fields_v1\npolicy_version: x\nenforcement: warn\ndefaults:\n  required: []\ntypes:\n  individual:\n    required: [name]\n",
			want: "resolves to no required fields",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadKYCRequiredFields(writePolicyFile(t, "kyc.yaml", test.body))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want it to mention %q", err, test.want)
			}
		})
	}
}

func TestKYCMissingPerCustomerType(t *testing.T) {
	policy := DefaultKYCRequiredFields()
	tests := []struct {
		customerType domain.CustomerType
		attributes   map[string]any
		want         []string
	}{
		{domain.CustomerTypeIndividual, map[string]any{}, []string{"name", "date_of_birth", "address"}},
		{domain.CustomerTypeIndividual, map[string]any{"name": "山田太郎", "date_of_birth": "1980-01-01", "address": "東京都"}, nil},
		{domain.CustomerTypeCorporateDomestic, map[string]any{"name": "株式会社A", "address": "東京都"}, []string{"corporate_number", "representative_name"}},
		{domain.CustomerTypeCorporateForeign, map[string]any{}, []string{"name", "address", "jurisdiction", "representative_name"}},
		{domain.CustomerTypeTrust, map[string]any{"name": "T", "trust_parties": []any{"a"}}, nil},
		{domain.CustomerTypePartnership, map[string]any{"name": "P"}, []string{"address", "representative_name"}},
		{domain.CustomerTypeNPO, map[string]any{"name": "N", "address": "A", "representative_name": "R"}, nil},
		{domain.CustomerTypeGovernment, map[string]any{"name": "G"}, []string{"jurisdiction"}},
		{domain.CustomerTypeForeignLegalArrangement, map[string]any{"name": "F"}, []string{"jurisdiction", "trust_parties"}},
		// A blank string is not identity evidence.
		{domain.CustomerTypeIndividual, map[string]any{"name": "  ", "date_of_birth": "1980-01-01", "address": "東京都"}, []string{"name"}},
		// An empty list is not a trust party.
		{domain.CustomerTypeTrust, map[string]any{"name": "T", "trust_parties": []any{}}, []string{"trust_parties"}},
	}
	for _, test := range tests {
		t.Run(string(test.customerType), func(t *testing.T) {
			got := policy.Missing(test.customerType, test.attributes)
			if len(got) != len(test.want) {
				t.Fatalf("Missing = %v; want %v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("Missing = %v; want %v", got, test.want)
				}
			}
		})
	}
}

// Every customer type must resolve to a requirement set, including the ones
// added after the original three.
func TestKYCCoversEveryCustomerType(t *testing.T) {
	policy := DefaultKYCRequiredFields()
	for _, customerType := range domain.AllCustomerTypes() {
		if len(policy.Required(customerType)) == 0 {
			t.Fatalf("%s has no required fields", customerType)
		}
	}
}

func TestEDDValidateRejectsBadSchedules(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "stages not increasing",
			body: "schema_version: edd_policy_v1\npolicy_version: x\ntrigger_tiers: [high]\ndue_days: 90\ntier_downgrade: retain_evidence\nstages:\n  - {name: a, after_days: 60, action: r}\n  - {name: b, after_days: 30, action: r}\n",
			want: "greater than the previous stage",
		},
		{
			name: "due before last stage",
			body: "schema_version: edd_policy_v1\npolicy_version: x\ntrigger_tiers: [high]\ndue_days: 10\ntier_downgrade: retain_evidence\nstages:\n  - {name: a, after_days: 30, action: r}\n",
			want: "due_days",
		},
		{
			name: "no stages",
			body: "schema_version: edd_policy_v1\npolicy_version: x\ntrigger_tiers: [high]\ndue_days: 90\ntier_downgrade: retain_evidence\nstages: []\n",
			want: "at least one stage",
		},
		{
			name: "unknown tier",
			body: "schema_version: edd_policy_v1\npolicy_version: x\ntrigger_tiers: [extreme]\ndue_days: 90\ntier_downgrade: retain_evidence\nstages:\n  - {name: a, after_days: 30, action: r}\n",
			want: "unknown tier",
		},
		{
			name: "unknown downgrade behaviour",
			body: "schema_version: edd_policy_v1\npolicy_version: x\ntrigger_tiers: [high]\ndue_days: 90\ntier_downgrade: forget\nstages:\n  - {name: a, after_days: 30, action: r}\n",
			want: "tier_downgrade",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LoadEDD(writePolicyFile(t, "edd.yaml", test.body)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want it to mention %q", err, test.want)
			}
		})
	}
}

// The boundary cases are what the read model gets wrong when the stage
// figures are duplicated: day 29 is not yet stage 1, day 30 is.
func TestEDDStageBoundaries(t *testing.T) {
	policy := DefaultEDD()
	tests := []struct {
		elapsed     int
		wantCurrent string
		wantNext    string
	}{
		{0, "", "stage1"},
		{29, "", "stage1"},
		{30, "stage1", "stage2"},
		{59, "stage1", "stage2"},
		{60, "stage2", "stage3"},
		{89, "stage2", "stage3"},
		{90, "stage3", ""},
		{365, "stage3", ""},
	}
	for _, test := range tests {
		current, next := policy.Stage(test.elapsed)
		gotCurrent, gotNext := "", ""
		if current != nil {
			gotCurrent = current.Name
		}
		if next != nil {
			gotNext = next.Name
		}
		if gotCurrent != test.wantCurrent || gotNext != test.wantNext {
			t.Fatalf("day %d: got (%q, %q); want (%q, %q)", test.elapsed, gotCurrent, gotNext, test.wantCurrent, test.wantNext)
		}
	}
}

func TestEDDOverdueIsMeasuredNotClamped(t *testing.T) {
	policy := DefaultEDD()
	requested := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dueDay := policy.DueAt(requested)
	if policy.IsOverdue(requested, dueDay) {
		t.Fatal("a window is not overdue on its due day")
	}
	if policy.OverdueDays(requested, dueDay) != 0 {
		t.Fatal("due day must report zero overdue days")
	}
	// The defect this replaces: 200 days late used to read the same as due
	// today because remaining days were clamped at zero.
	late := dueDay.AddDate(0, 0, 200)
	if !policy.IsOverdue(requested, late) {
		t.Fatal("200 days past due must be overdue")
	}
	if got := policy.OverdueDays(requested, late); got != 200 {
		t.Fatalf("OverdueDays = %d; want 200", got)
	}
}

func TestCDDRuleSelectionResolvesDeterministically(t *testing.T) {
	body := `schema_version: cdd_rule_selection_v1
policy_version: test
selection_authority: server
rules:
  - match: {customer_type: [corporate_foreign]}
    rule_set_id: cdd_cross_border
    priority: 100
  - match: {jurisdiction: [JP]}
    rule_set_id: cdd_basic
    priority: 10
`
	policy, err := LoadCDDRuleSelection(writePolicyFile(t, "cdd.yaml", body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	foreign := &domain.Customer{CustomerType: domain.CustomerTypeCorporateForeign, CountryCode: "JP"}
	candidates := policy.Applicable(foreign)
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d; want 2", len(candidates))
	}
	// Highest priority wins and is the one marked recommended, regardless of
	// the order the rules appear in the file.
	if candidates[0].RuleSetID != "cdd_cross_border" || !candidates[0].Recommended {
		t.Fatalf("top candidate = %+v; want cdd_cross_border recommended", candidates[0])
	}
	if candidates[0].MatchedOn == "" {
		t.Fatal("a candidate must explain which clause matched it")
	}
	if got := policy.Resolve(foreign); got != "cdd_cross_border" {
		t.Fatalf("Resolve = %q; want cdd_cross_border", got)
	}
	domestic := &domain.Customer{CustomerType: domain.CustomerTypeIndividual, CountryCode: "US"}
	if got := policy.Resolve(domestic); got != "" {
		t.Fatalf("Resolve for an unmatched customer = %q; want the empty fallback", got)
	}
}

func TestCDDRuleSelectionRejectsAmbiguity(t *testing.T) {
	body := `schema_version: cdd_rule_selection_v1
policy_version: test
selection_authority: server
rules:
  - match: {jurisdiction: [JP]}
    rule_set_id: a
    priority: 10
  - match: {jurisdiction: [US]}
    rule_set_id: b
    priority: 10
`
	if _, err := LoadCDDRuleSelection(writePolicyFile(t, "cdd.yaml", body)); err == nil || !strings.Contains(err.Error(), "repeat priority") {
		t.Fatalf("error = %v; want a duplicate-priority rejection", err)
	}
}

func TestTravelRuleAssessThresholdBoundary(t *testing.T) {
	policy := DefaultTravelRule()
	now := time.Now()
	transfer := func() *domain.Transaction {
		return &domain.Transaction{
			Channel:   "crypto",
			Direction: domain.DirectionOutbound,
			Counterparty: &domain.Counterparty{
				CounterpartyType: domain.CounterpartyTypeVASP,
			},
		}
	}
	below := policy.Assess(transfer(), 99999, now)
	if below.Applicable {
		t.Fatal("a transfer below the threshold is not applicable")
	}
	if below.ReasonCode != TravelRuleReasonBelowThreshold {
		t.Fatalf("reason = %q; want below_threshold", below.ReasonCode)
	}
	// The threshold is inclusive: exactly 100,000 JPY is covered.
	atThreshold := policy.Assess(transfer(), 100000, now)
	if !atThreshold.Applicable {
		t.Fatal("a transfer at exactly the threshold must be applicable")
	}
	if len(atThreshold.MissingFields) == 0 {
		t.Fatal("a VASP transfer with no evidence must report missing fields")
	}
	if atThreshold.PolicyVersion == "" || atThreshold.Threshold != 100000 || atThreshold.Currency != "JPY" {
		t.Fatalf("assessment must pin the policy: %+v", atThreshold)
	}
}

func TestTravelRuleAssessNonCoveredActivity(t *testing.T) {
	policy := DefaultTravelRule()
	now := time.Now()
	fiat := &domain.Transaction{Channel: "bank_transfer", Direction: domain.DirectionOutbound}
	assessment := policy.Assess(fiat, 5_000_000, now)
	if assessment.Applicable {
		t.Fatal("a fiat bank transfer is not a covered channel")
	}
	if assessment.ReasonCode != TravelRuleReasonFiatOnly {
		t.Fatalf("reason = %q; want fiat_only", assessment.ReasonCode)
	}
}

func TestTravelRuleMissingEvidenceResolvesStructuredCounterparty(t *testing.T) {
	policy := DefaultTravelRule()
	transaction := &domain.Transaction{
		Channel:   "crypto",
		Direction: domain.DirectionOutbound,
		Counterparty: &domain.Counterparty{
			CounterpartyType: domain.CounterpartyTypeVASP,
			Originator:       domain.CounterpartyParty{Name: "A", AccountNumber: "1", VASPName: "VA"},
			Beneficiary:      domain.CounterpartyParty{Name: "B", AccountNumber: "2", VASPName: "VB"},
		},
	}
	if missing := policy.MissingEvidence(domain.CounterpartyTypeVASP, transaction); len(missing) != 0 {
		t.Fatalf("MissingEvidence = %v; want none when the counterparty carries every field", missing)
	}
	// The free-form evidence map is an equally valid source for a field.
	viaEvidence := &domain.Transaction{
		Channel:            "crypto",
		Direction:          domain.DirectionOutbound,
		Counterparty:       &domain.Counterparty{CounterpartyType: domain.CounterpartyTypeUnknown},
		TravelRuleEvidence: map[string]any{"originator_name": "A", "beneficiary_account": "2"},
	}
	if missing := policy.MissingEvidence(domain.CounterpartyTypeUnknown, viaEvidence); len(missing) != 0 {
		t.Fatalf("MissingEvidence = %v; want none when evidence supplies the fields", missing)
	}
}

func TestTravelRuleValidateRejectsBadDocuments(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"zero threshold", "schema_version: travel_rule_v1\npolicy_version: x\nthreshold_amount: 0\nthreshold_currency: JPY\ncovered_channels: [crypto]\ncovered_directions: [outbound]\nnot_applicable_reasons: [below_threshold]\nassertion_authority: client\nincomplete_routing: none\n", "threshold_amount"},
		{"bad currency", "schema_version: travel_rule_v1\npolicy_version: x\nthreshold_amount: 1\nthreshold_currency: jpy\ncovered_channels: [crypto]\ncovered_directions: [outbound]\nnot_applicable_reasons: [below_threshold]\nassertion_authority: client\nincomplete_routing: none\n", "threshold_currency"},
		{"no channels", "schema_version: travel_rule_v1\npolicy_version: x\nthreshold_amount: 1\nthreshold_currency: JPY\ncovered_channels: []\ncovered_directions: [outbound]\nnot_applicable_reasons: [below_threshold]\nassertion_authority: client\nincomplete_routing: none\n", "covered_channels"},
		{"unknown direction", "schema_version: travel_rule_v1\npolicy_version: x\nthreshold_amount: 1\nthreshold_currency: JPY\ncovered_channels: [crypto]\ncovered_directions: [sideways]\nnot_applicable_reasons: [below_threshold]\nassertion_authority: client\nincomplete_routing: none\n", "unknown direction"},
		{"unknown counterparty type", "schema_version: travel_rule_v1\npolicy_version: x\nthreshold_amount: 1\nthreshold_currency: JPY\ncovered_channels: [crypto]\ncovered_directions: [outbound]\napplicable_counterparty_types: [bank]\nnot_applicable_reasons: [below_threshold]\nassertion_authority: client\nincomplete_routing: none\n", "unknown type"},
		{"no reasons", "schema_version: travel_rule_v1\npolicy_version: x\nthreshold_amount: 1\nthreshold_currency: JPY\ncovered_channels: [crypto]\ncovered_directions: [outbound]\nnot_applicable_reasons: []\nassertion_authority: client\nincomplete_routing: none\n", "not_applicable_reasons"},
		{"unknown assertion authority", "schema_version: travel_rule_v1\npolicy_version: x\nthreshold_amount: 1\nthreshold_currency: JPY\ncovered_channels: [crypto]\ncovered_directions: [outbound]\nnot_applicable_reasons: [below_threshold]\nassertion_authority: regulator\nincomplete_routing: none\n", "assertion_authority"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LoadTravelRule(writePolicyFile(t, "tr.yaml", test.body)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want it to mention %q", err, test.want)
			}
		})
	}
}

func TestScreeningReadinessThresholdsAndRequirements(t *testing.T) {
	body := `schema_version: screening_readiness_v1
policy_version: test
default_freshness_seconds: 259200
mark_runs_degraded: true
gate_screening_runs: false
sources:
  - {list_id: ofac_sdn, required: true, freshness_seconds: 86400}
  - {list_id: pep_provider, required: false}
`
	policy, err := LoadScreeningReadiness(writePolicyFile(t, "sr.yaml", body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := policy.ThresholdFor("ofac_sdn"); got != 24*time.Hour {
		t.Fatalf("ofac threshold = %v; want 24h", got)
	}
	// An unconfigured per-source window falls back to the policy default.
	if got := policy.ThresholdFor("pep_provider"); got != 72*time.Hour {
		t.Fatalf("pep threshold = %v; want the 72h default", got)
	}
	if got := policy.ThresholdFor("not_configured"); got != 72*time.Hour {
		t.Fatalf("unknown source threshold = %v; want the 72h default", got)
	}
	if !policy.Required("ofac_sdn") || policy.Required("pep_provider") {
		t.Fatal("required flags did not round-trip")
	}
	// An unconfigured source cannot be required: nothing depends on it.
	if policy.Required("not_configured") {
		t.Fatal("an unconfigured source must not be required")
	}
	if ids := policy.SourceIDs(); len(ids) != 2 || ids[0] != "ofac_sdn" {
		t.Fatalf("SourceIDs = %v; want the configured cardinality in policy order", ids)
	}
}

func TestScreeningReadinessRejectsNoRequiredSource(t *testing.T) {
	body := "schema_version: screening_readiness_v1\npolicy_version: x\ndefault_freshness_seconds: 100\nsources:\n  - {list_id: a, required: false}\n"
	if _, err := LoadScreeningReadiness(writePolicyFile(t, "sr.yaml", body)); err == nil || !strings.Contains(err.Error(), "must be required") {
		t.Fatalf("error = %v; want a no-required-source rejection", err)
	}
}

func TestLoadReportsFileSourceAndFailsClosed(t *testing.T) {
	path := writePolicyFile(t, "kyc.yaml", "schema_version: kyc_required_fields_v1\npolicy_version: from-file\nenforcement: reject\ndefaults:\n  required: [name]\n")
	set, err := Load(Paths{KYCRequiredFields: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	descriptor, _ := set.Describe(NameKYCRequiredFields)
	if descriptor.Source != "file" || descriptor.PolicyVersion != "from-file" {
		t.Fatalf("descriptor = %+v; want the file to win", descriptor)
	}
	if set.KYC().Enforce() != KYCEnforcementReject {
		t.Fatal("file enforcement did not take effect")
	}
	// A policy the operator meant to apply but that cannot be parsed is a
	// configuration error, not a reason to silently score under other rules.
	bad := writePolicyFile(t, "bad.yaml", "schema_version: wrong\n")
	if _, err := Load(Paths{KYCRequiredFields: bad}); err == nil {
		t.Fatal("Load must fail on an invalid document rather than fall back")
	}
	if _, err := Load(Paths{EDD: filepath.Join(t.TempDir(), "missing.yaml")}); err == nil {
		t.Fatal("Load must fail when a named policy file does not exist")
	}
}

func TestSetDigestsAreNamespaced(t *testing.T) {
	set, err := Load(Paths{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	digests := set.Digests()
	for _, name := range Names() {
		if digests["policy:"+name] == "" {
			t.Fatalf("missing digest for %s", name)
		}
	}
	if _, ok := set.Document("nonexistent"); ok {
		t.Fatal("an unknown policy name must not resolve")
	}
}
