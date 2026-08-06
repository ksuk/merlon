package policy

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ksuk/merlon/api/internal/domain"
)

const cddSelectionSchemaVersion = "cdd_rule_selection_v1"

// CDDSelectionAuthority decides who chooses the CDD rule set applied to a
// manual rescore.
type CDDSelectionAuthority string

const (
	// CDDAuthorityServer resolves the rule set from this policy whenever the
	// caller does not name one.
	CDDAuthorityServer CDDSelectionAuthority = "server"
	// CDDAuthorityClient lets the caller name any active rule set.
	CDDAuthorityClient CDDSelectionAuthority = "client"
)

// CDDMatch is the customer predicate for one applicability rule. An empty
// list matches every value for that dimension; all populated dimensions must
// match.
type CDDMatch struct {
	CustomerType []domain.CustomerType `yaml:"customer_type,omitempty" json:"customer_type,omitempty"`
	ProductType  []string              `yaml:"product_type,omitempty" json:"product_type,omitempty"`
	Jurisdiction []string              `yaml:"jurisdiction,omitempty" json:"jurisdiction,omitempty"`
}

// CDDSelectionRule binds a customer predicate to a CDD rule set.
type CDDSelectionRule struct {
	Match     CDDMatch `yaml:"match" json:"match"`
	RuleSetID string   `yaml:"rule_set_id" json:"rule_set_id"`
	Priority  int      `yaml:"priority" json:"priority"`
}

// CDDRuleSelectionPolicy makes CDD rule-set applicability configuration
// rather than a positional accident (DR-15). Before it, the UI applied
// whichever active rule the API happened to list first.
type CDDRuleSelectionPolicy struct {
	SchemaVersion      string                `yaml:"schema_version" json:"schema_version"`
	PolicyVersion      string                `yaml:"policy_version" json:"policy_version"`
	DefaultRuleSetID   string                `yaml:"default_rule_set_id" json:"default_rule_set_id"`
	SelectionAuthority CDDSelectionAuthority `yaml:"selection_authority" json:"selection_authority"`
	Rules              []CDDSelectionRule    `yaml:"rules" json:"rules"`
}

// DefaultCDDRuleSelection ships no rules, so an unconfigured deployment
// keeps resolving through the single active CDD rule set exactly as before.
func DefaultCDDRuleSelection() *CDDRuleSelectionPolicy {
	return &CDDRuleSelectionPolicy{
		SchemaVersion:      cddSelectionSchemaVersion,
		PolicyVersion:      "2026-08-06-default",
		SelectionAuthority: CDDAuthorityServer,
	}
}

// LoadCDDRuleSelection reads the policy from path, or returns the default
// when path is blank.
func LoadCDDRuleSelection(path string) (*CDDRuleSelectionPolicy, error) {
	var loaded CDDRuleSelectionPolicy
	present, err := readPolicy("cdd rule selection", path, &loaded)
	if err != nil {
		return nil, err
	}
	if !present {
		return DefaultCDDRuleSelection(), nil
	}
	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("validate cdd rule selection policy %q: %w", path, err)
	}
	return &loaded, nil
}

var jurisdictionPattern = regexp.MustCompile(`^[A-Z]{2}$`)

// Validate refuses ambiguity: two rules at the same priority would make the
// applied policy depend on file order, which is exactly the non-determinism
// this document replaces.
func (p *CDDRuleSelectionPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	if err := requireVersion("cdd rule selection", p.SchemaVersion, cddSelectionSchemaVersion, p.PolicyVersion); err != nil {
		return err
	}
	switch p.SelectionAuthority {
	case CDDAuthorityServer, CDDAuthorityClient:
	default:
		return fmt.Errorf("selection_authority must be server or client")
	}
	priorities := make([]string, 0, len(p.Rules))
	for i, rule := range p.Rules {
		if strings.TrimSpace(rule.RuleSetID) == "" {
			return fmt.Errorf("rules[%d].rule_set_id is required", i)
		}
		for _, customerType := range rule.Match.CustomerType {
			if !domain.IsValidCustomerType(customerType) {
				return fmt.Errorf("rules[%d].match.customer_type contains an unknown type %q", i, customerType)
			}
		}
		for _, jurisdiction := range rule.Match.Jurisdiction {
			if !jurisdictionPattern.MatchString(jurisdiction) {
				return fmt.Errorf("rules[%d].match.jurisdiction %q must be a two-letter uppercase code", i, jurisdiction)
			}
		}
		if len(rule.Match.CustomerType) == 0 && len(rule.Match.ProductType) == 0 && len(rule.Match.Jurisdiction) == 0 {
			return fmt.Errorf("rules[%d].match must constrain at least one dimension", i)
		}
		priorities = append(priorities, fmt.Sprintf("%d", rule.Priority))
	}
	if value, dup := duplicates(priorities); dup {
		return fmt.Errorf("rules repeat priority %s; priorities must be unique so resolution is deterministic", value)
	}
	return nil
}

func (p *CDDRuleSelectionPolicy) resolved() *CDDRuleSelectionPolicy {
	if p == nil {
		return DefaultCDDRuleSelection()
	}
	return p
}

// Candidate is one applicable rule set with the clause that selected it, so
// the UI can explain the recommendation rather than assert it.
type Candidate struct {
	RuleSetID   string `json:"rule_set_id"`
	Priority    int    `json:"priority"`
	MatchedOn   string `json:"matched_on"`
	Recommended bool   `json:"recommended"`
}

// Applicable returns every rule set matching the customer, highest priority
// first, with the top entry marked recommended.
func (p *CDDRuleSelectionPolicy) Applicable(customer *domain.Customer) []Candidate {
	policy := p.resolved()
	if customer == nil {
		return nil
	}
	var out []Candidate
	for _, rule := range policy.Rules {
		matched, clause := matchCDDRule(rule.Match, customer)
		if !matched {
			continue
		}
		out = append(out, Candidate{RuleSetID: rule.RuleSetID, Priority: rule.Priority, MatchedOn: clause})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	if len(out) > 0 {
		out[0].Recommended = true
	}
	return out
}

// Resolve returns the rule set the policy selects for the customer, or an
// empty string when the policy expresses no opinion and the caller should
// fall back to the single active CDD rule set.
func (p *CDDRuleSelectionPolicy) Resolve(customer *domain.Customer) string {
	if candidates := p.Applicable(customer); len(candidates) > 0 {
		return candidates[0].RuleSetID
	}
	return strings.TrimSpace(p.resolved().DefaultRuleSetID)
}

// ServerResolves reports whether the server picks the rule set when the
// caller does not name one.
func (p *CDDRuleSelectionPolicy) ServerResolves() bool {
	return p.resolved().SelectionAuthority == CDDAuthorityServer
}

func matchCDDRule(match CDDMatch, customer *domain.Customer) (bool, string) {
	var clauses []string
	if len(match.CustomerType) > 0 {
		if !slices.Contains(match.CustomerType, customer.CustomerType) {
			return false, ""
		}
		clauses = append(clauses, "customer_type="+string(customer.CustomerType))
	}
	if len(match.Jurisdiction) > 0 {
		if !slices.Contains(match.Jurisdiction, strings.ToUpper(customer.CountryCode)) {
			return false, ""
		}
		clauses = append(clauses, "jurisdiction="+strings.ToUpper(customer.CountryCode))
	}
	if len(match.ProductType) > 0 {
		matchedProduct := ""
		for _, product := range customer.ProductTypes {
			if slices.Contains(match.ProductType, product) {
				matchedProduct = product
				break
			}
		}
		if matchedProduct == "" {
			return false, ""
		}
		clauses = append(clauses, "product_type="+matchedProduct)
	}
	return true, strings.Join(clauses, ", ")
}

// Version reports the policy version for audit records.
func (p *CDDRuleSelectionPolicy) Version() string {
	if p == nil || strings.TrimSpace(p.PolicyVersion) == "" {
		return "unknown"
	}
	return p.PolicyVersion
}

func (p *CDDRuleSelectionPolicy) versionInfo() (string, string) {
	if p == nil {
		return cddSelectionSchemaVersion, "unknown"
	}
	return p.SchemaVersion, p.Version()
}
