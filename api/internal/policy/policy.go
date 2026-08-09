// Package policy holds the versioned, operator-editable AML policy documents
// that the Fail-Alert and Configuration-as-the-Product principles require to
// live outside Go code (ADR-0016).
//
// Every policy follows the same shape, established by
// casemgmt.PriorityPolicy: a pinned schema_version, a required
// policy_version, an in-code Default*() fallback, a Load*(path) that returns
// the default for a blank path, and a Validate() that refuses a document the
// engine cannot reason about.
//
// Each accessor tolerates a nil receiver and returns the in-code default, so
// a Server assembled without policies behaves exactly as it did before the
// package existed.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Paths names the file for each policy document. A blank entry selects the
// in-code default for that policy.
type Paths struct {
	KYCRequiredFields  string
	EDD                string
	CDDRuleSelection   string
	TravelRule         string
	ScreeningReadiness string
	SLA                string
}

// Set is the assembled policy bundle held by the server. A nil *Set is valid
// and yields defaults throughout; this is what keeps handler tests that build
// a Server without policies working unchanged.
type Set struct {
	kyc                *KYCRequiredFieldsPolicy
	edd                *EDDPolicy
	cddRuleSelection   *CDDRuleSelectionPolicy
	travelRule         *TravelRulePolicy
	screeningReadiness *ScreeningReadinessPolicy
	sla                *SLAPolicy

	digests map[string]string
	sources map[string]string
}

// Load reads every policy named in paths. It fails on the first invalid
// document rather than silently falling back, because a policy the operator
// meant to apply but that cannot be parsed is a configuration error, not a
// reason to score customers under different rules than intended.
func Load(paths Paths) (*Set, error) {
	set := &Set{
		digests: map[string]string{},
		sources: map[string]string{},
	}
	var err error
	if set.kyc, err = LoadKYCRequiredFields(paths.KYCRequiredFields); err != nil {
		return nil, err
	}
	if set.edd, err = LoadEDD(paths.EDD); err != nil {
		return nil, err
	}
	if set.cddRuleSelection, err = LoadCDDRuleSelection(paths.CDDRuleSelection); err != nil {
		return nil, err
	}
	if set.travelRule, err = LoadTravelRule(paths.TravelRule); err != nil {
		return nil, err
	}
	if set.screeningReadiness, err = LoadScreeningReadiness(paths.ScreeningReadiness); err != nil {
		return nil, err
	}
	if set.sla, err = LoadSLA(paths.SLA); err != nil {
		return nil, err
	}
	set.record(NameKYCRequiredFields, paths.KYCRequiredFields, set.kyc)
	set.record(NameEDD, paths.EDD, set.edd)
	set.record(NameCDDRuleSelection, paths.CDDRuleSelection, set.cddRuleSelection)
	set.record(NameTravelRule, paths.TravelRule, set.travelRule)
	set.record(NameScreeningReadiness, paths.ScreeningReadiness, set.screeningReadiness)
	set.record(NameSLA, paths.SLA, set.sla)
	return set, nil
}

// Policy document names, used by the read-only /api/v1/policies surface and
// by the startup digest map.
const (
	NameKYCRequiredFields  = "kyc_required_fields"
	NameEDD                = "edd"
	NameCDDRuleSelection   = "cdd_rule_selection"
	NameTravelRule         = "travel_rule"
	NameScreeningReadiness = "screening_readiness"
	NameSLA                = "sla"
)

// Names lists every policy in a stable order.
func Names() []string {
	return []string{
		NameKYCRequiredFields,
		NameEDD,
		NameCDDRuleSelection,
		NameTravelRule,
		NameScreeningReadiness,
		NameSLA,
	}
}

func (s *Set) record(name, path string, document any) {
	if strings.TrimSpace(path) == "" {
		s.sources[name] = "default"
	} else {
		s.sources[name] = "file"
	}
	s.digests[name] = digest(document)
}

// SLA returns the service-level policy, or the in-code default. The default
// declares no rules, so an unconfigured deployment reports not_configured
// rather than inventing a deadline (ADR-0024, DR-07).
func (s *Set) SLA() *SLAPolicy {
	if s == nil || s.sla == nil {
		return DefaultSLAPolicy()
	}
	return s.sla
}

// KYC returns the KYC required-field policy, or the in-code default.
func (s *Set) KYC() *KYCRequiredFieldsPolicy {
	if s == nil || s.kyc == nil {
		return DefaultKYCRequiredFields()
	}
	return s.kyc
}

// EDD returns the EDD lifecycle policy, or the in-code default.
func (s *Set) EDD() *EDDPolicy {
	if s == nil || s.edd == nil {
		return DefaultEDD()
	}
	return s.edd
}

// CDDRuleSelection returns the CDD rule-set applicability policy, or the
// in-code default.
func (s *Set) CDDRuleSelection() *CDDRuleSelectionPolicy {
	if s == nil || s.cddRuleSelection == nil {
		return DefaultCDDRuleSelection()
	}
	return s.cddRuleSelection
}

// TravelRule returns the Travel Rule applicability policy, or the in-code
// default.
func (s *Set) TravelRule() *TravelRulePolicy {
	if s == nil || s.travelRule == nil {
		return DefaultTravelRule()
	}
	return s.travelRule
}

// ScreeningReadiness returns the screening source readiness policy, or the
// in-code default.
func (s *Set) ScreeningReadiness() *ScreeningReadinessPolicy {
	if s == nil || s.screeningReadiness == nil {
		return DefaultScreeningReadiness()
	}
	return s.screeningReadiness
}

// Document returns the parsed policy document for name, for the read-only
// policy API. The second return is false for an unknown name.
func (s *Set) Document(name string) (any, bool) {
	switch name {
	case NameKYCRequiredFields:
		return s.KYC(), true
	case NameEDD:
		return s.EDD(), true
	case NameCDDRuleSelection:
		return s.CDDRuleSelection(), true
	case NameTravelRule:
		return s.TravelRule(), true
	case NameScreeningReadiness:
		return s.ScreeningReadiness(), true
	case NameSLA:
		return s.SLA(), true
	default:
		return nil, false
	}
}

// Descriptor summarises one policy for the listing endpoint and for the
// startup digest log.
type Descriptor struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schema_version"`
	PolicyVersion string `json:"policy_version"`
	Digest        string `json:"digest"`
	Source        string `json:"source"`
}

// Describe returns the descriptor for name.
func (s *Set) Describe(name string) (Descriptor, bool) {
	document, ok := s.Document(name)
	if !ok {
		return Descriptor{}, false
	}
	schemaVersion, policyVersion := versions(document)
	return Descriptor{
		Name:          name,
		SchemaVersion: schemaVersion,
		PolicyVersion: policyVersion,
		Digest:        s.digestFor(name, document),
		Source:        s.sourceFor(name),
	}, true
}

// Descriptors returns every policy descriptor in Names() order.
func (s *Set) Descriptors() []Descriptor {
	out := make([]Descriptor, 0, len(Names()))
	for _, name := range Names() {
		if descriptor, ok := s.Describe(name); ok {
			out = append(out, descriptor)
		}
	}
	return out
}

// Digests returns policy digests keyed by "policy:<name>", ready to merge
// into the process-wide configuration digest map that flows onto batch and
// screening run records.
func (s *Set) Digests() map[string]string {
	out := map[string]string{}
	for _, name := range Names() {
		document, _ := s.Document(name)
		out["policy:"+name] = s.digestFor(name, document)
	}
	return out
}

func (s *Set) digestFor(name string, document any) string {
	if s != nil && s.digests != nil {
		if value, ok := s.digests[name]; ok {
			return value
		}
	}
	return digest(document)
}

func (s *Set) sourceFor(name string) string {
	if s != nil && s.sources != nil {
		if value, ok := s.sources[name]; ok {
			return value
		}
	}
	return "default"
}

func versions(document any) (string, string) {
	type versioned interface {
		versionInfo() (string, string)
	}
	if v, ok := document.(versioned); ok {
		return v.versionInfo()
	}
	return "", ""
}

// digest pins a policy document by content so an operator can prove which
// rules produced a given decision. YAML is used rather than JSON because it
// is the authored form and marshals map keys in sorted order.
func digest(document any) string {
	data, err := yaml.Marshal(document)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// readPolicy loads and unmarshals path into out. A blank path is the caller's
// signal to use the in-code default and is reported through the boolean.
func readPolicy(kind, path string, out any) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s policy %q: %w", kind, path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("parse %s policy %q: %w", kind, path, err)
	}
	return true, nil
}

func requireVersion(kind, schemaVersion, wantSchema, policyVersion string) error {
	if schemaVersion != wantSchema {
		return fmt.Errorf("%s: schema_version must be %s", kind, wantSchema)
	}
	if strings.TrimSpace(policyVersion) == "" {
		return fmt.Errorf("%s: policy_version is required", kind)
	}
	return nil
}

// duplicates reports the first value that appears more than once.
func duplicates(values []string) (string, bool) {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return value, true
		}
		seen[value] = true
	}
	return "", false
}
