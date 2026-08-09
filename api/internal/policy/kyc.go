package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ksuk/merlon/api/internal/domain"
)

const kycSchemaVersion = "kyc_required_fields_v1"

// KYCEnforcement decides what happens when a customer record is missing a
// field the policy requires for its type.
type KYCEnforcement string

const (
	// KYCEnforcementWarn accepts the write and reports the gap, so an
	// institution can adopt the policy without breaking an existing feed.
	KYCEnforcementWarn KYCEnforcement = "warn"
	// KYCEnforcementReject refuses the write.
	KYCEnforcementReject KYCEnforcement = "reject"
)

// KYCTypeRequirements is the required/recommended field split for one
// customer type.
type KYCTypeRequirements struct {
	Required    []string `yaml:"required" json:"required"`
	Recommended []string `yaml:"recommended,omitempty" json:"recommended,omitempty"`
}

// KYCRequiredFieldsPolicy expresses 取引時確認 identity requirements per
// customer type (DR-13). The fields are attribute keys on the customer
// record; the policy deliberately does not know how they are collected.
type KYCRequiredFieldsPolicy struct {
	SchemaVersion string                                      `yaml:"schema_version" json:"schema_version"`
	PolicyVersion string                                      `yaml:"policy_version" json:"policy_version"`
	Enforcement   KYCEnforcement                              `yaml:"enforcement" json:"enforcement"`
	Defaults      KYCTypeRequirements                         `yaml:"defaults" json:"defaults"`
	Types         map[domain.CustomerType]KYCTypeRequirements `yaml:"types" json:"types"`
}

// DefaultKYCRequiredFields ships warn enforcement so adopting the policy
// never rejects a customer an existing deployment could previously create.
// An institution moves to reject by editing one line of YAML.
func DefaultKYCRequiredFields() *KYCRequiredFieldsPolicy {
	return &KYCRequiredFieldsPolicy{
		SchemaVersion: kycSchemaVersion,
		PolicyVersion: "2026-08-06-default",
		Enforcement:   KYCEnforcementWarn,
		Defaults: KYCTypeRequirements{
			Required:    []string{"name"},
			Recommended: []string{"name_kana", "address"},
		},
		Types: map[domain.CustomerType]KYCTypeRequirements{
			domain.CustomerTypeIndividual: {
				Required:    []string{"name", "date_of_birth", "address"},
				Recommended: []string{"name_kana", "occupation", "nationality"},
			},
			domain.CustomerTypeCorporateDomestic: {
				Required:    []string{"name", "address", "corporate_number", "representative_name"},
				Recommended: []string{"name_kana", "industry"},
			},
			domain.CustomerTypeCorporateForeign: {
				Required:    []string{"name", "address", "jurisdiction", "representative_name"},
				Recommended: []string{"beneficial_owners", "industry"},
			},
			domain.CustomerTypeTrust: {
				Required:    []string{"name", "trust_parties"},
				Recommended: []string{"address", "jurisdiction"},
			},
			domain.CustomerTypePartnership: {
				Required:    []string{"name", "address", "representative_name"},
				Recommended: []string{"beneficial_owners"},
			},
			domain.CustomerTypeNPO: {
				Required:    []string{"name", "address", "representative_name"},
				Recommended: []string{"name_kana"},
			},
			// 犯収法上の取引時確認義務は原則免除されるため、識別に必要な
			// 最小限のみを必須とする。制裁リスト照合は別途実施される。
			domain.CustomerTypeGovernment: {
				Required:    []string{"name", "jurisdiction"},
				Recommended: []string{"address"},
			},
			domain.CustomerTypeForeignLegalArrangement: {
				Required:    []string{"name", "jurisdiction", "trust_parties"},
				Recommended: []string{"address"},
			},
		},
	}
}

// LoadKYCRequiredFields reads the policy from path, or returns the default
// when path is blank.
func LoadKYCRequiredFields(path string) (*KYCRequiredFieldsPolicy, error) {
	var loaded KYCRequiredFieldsPolicy
	present, err := readPolicy("kyc required fields", path, &loaded)
	if err != nil {
		return nil, err
	}
	if !present {
		return DefaultKYCRequiredFields(), nil
	}
	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("validate kyc required fields policy %q: %w", path, err)
	}
	return &loaded, nil
}

var kycFieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Validate refuses a document the engine cannot apply. The strictest check is
// that every customer type must resolve to a requirement set: a type that
// silently falls through to nothing is exactly the defect this policy exists
// to close.
func (p *KYCRequiredFieldsPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	if err := requireVersion("kyc required fields", p.SchemaVersion, kycSchemaVersion, p.PolicyVersion); err != nil {
		return err
	}
	switch p.Enforcement {
	case KYCEnforcementWarn, KYCEnforcementReject:
	default:
		return fmt.Errorf("enforcement must be warn or reject")
	}
	if err := validateKYCFields("defaults", p.Defaults); err != nil {
		return err
	}
	for _, key := range sortedCustomerTypes(p.Types) {
		customerType := domain.CustomerType(key)
		if !domain.IsValidCustomerType(customerType) {
			return fmt.Errorf("types.%s is not a known customer type", key)
		}
		if err := validateKYCFields("types."+key, p.Types[customerType]); err != nil {
			return err
		}
	}
	for _, customerType := range domain.AllCustomerTypes() {
		if len(p.requirementsFor(customerType).Required) == 0 {
			return fmt.Errorf("types.%s resolves to no required fields; add it or give defaults.required a value", customerType)
		}
	}
	return nil
}

func validateKYCFields(label string, requirements KYCTypeRequirements) error {
	required := map[string]bool{}
	for _, field := range requirements.Required {
		if !kycFieldPattern.MatchString(field) {
			return fmt.Errorf("%s.required contains an invalid field name %q", label, field)
		}
		if required[field] {
			return fmt.Errorf("%s.required repeats %q", label, field)
		}
		required[field] = true
	}
	for _, field := range requirements.Recommended {
		if !kycFieldPattern.MatchString(field) {
			return fmt.Errorf("%s.recommended contains an invalid field name %q", label, field)
		}
		if required[field] {
			return fmt.Errorf("%s lists %q as both required and recommended", label, field)
		}
	}
	return nil
}

func sortedCustomerTypes(in map[domain.CustomerType]KYCTypeRequirements) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, string(key))
	}
	sort.Strings(out)
	return out
}

func (p *KYCRequiredFieldsPolicy) requirementsFor(customerType domain.CustomerType) KYCTypeRequirements {
	if p == nil {
		return KYCTypeRequirements{}
	}
	if requirements, ok := p.Types[customerType]; ok {
		return requirements
	}
	return p.Defaults
}

// Required lists the attribute keys a customer of this type must carry.
func (p *KYCRequiredFieldsPolicy) Required(customerType domain.CustomerType) []string {
	return append([]string(nil), p.requirementsFor(customerType).Required...)
}

// Recommended lists the attribute keys an operator should collect but that
// do not gate acceptance.
func (p *KYCRequiredFieldsPolicy) Recommended(customerType domain.CustomerType) []string {
	return append([]string(nil), p.requirementsFor(customerType).Recommended...)
}

// Missing returns the required attribute keys that attributes does not
// supply, in policy order. A key whose value is present but blank counts as
// missing: a KYC field recorded as an empty string carries no identity.
func (p *KYCRequiredFieldsPolicy) Missing(customerType domain.CustomerType, attributes map[string]any) []string {
	var missing []string
	for _, field := range p.requirementsFor(customerType).Required {
		if !attributeSatisfied(attributes, field) {
			missing = append(missing, field)
		}
	}
	return missing
}

func attributeSatisfied(attributes map[string]any, field string) bool {
	value, ok := attributes[field]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// Enforce reports whether a missing field must reject the write.
func (p *KYCRequiredFieldsPolicy) Enforce() KYCEnforcement {
	if p == nil || p.Enforcement == "" {
		return KYCEnforcementWarn
	}
	return p.Enforcement
}

// Version reports the policy version for audit records.
func (p *KYCRequiredFieldsPolicy) Version() string {
	if p == nil || strings.TrimSpace(p.PolicyVersion) == "" {
		return "unknown"
	}
	return p.PolicyVersion
}

func (p *KYCRequiredFieldsPolicy) versionInfo() (string, string) {
	if p == nil {
		return kycSchemaVersion, "unknown"
	}
	return p.SchemaVersion, p.Version()
}
