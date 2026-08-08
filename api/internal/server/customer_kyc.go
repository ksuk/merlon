package server

import (
	"net/http"
	"strings"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
)

// validCustomerTypes is the single list both the type check and its error
// message come from. They had drifted: isValidCustomerType accepted eight
// types while the message named three, so an operator told to send one of
// "individual, corporate_domestic, corporate_foreign" had no way to learn
// that trust, partnership, npo, government and foreign_legal_arrangement were
// equally acceptable.
func validCustomerTypes() []domain.CustomerType {
	return []domain.CustomerType{
		domain.CustomerTypeIndividual,
		domain.CustomerTypeCorporateDomestic,
		domain.CustomerTypeCorporateForeign,
		domain.CustomerTypeTrust,
		domain.CustomerTypePartnership,
		domain.CustomerTypeNPO,
		domain.CustomerTypeGovernment,
		domain.CustomerTypeForeignLegalArrangement,
	}
}

func customerTypeErrorMessage() string {
	names := make([]string, 0, len(validCustomerTypes()))
	for _, ct := range validCustomerTypes() {
		names = append(names, string(ct))
	}
	return "customer_type must be one of: " + strings.Join(names, ", ")
}

// customerWithKYC is the customer response plus the identity gaps the KYC
// policy names for its type. It is additive: every existing field keeps its
// place, and kyc_missing_fields is absent when nothing is missing.
type customerWithKYC struct {
	*domain.Customer
	KYCMissingFields []string `json:"kyc_missing_fields,omitempty"`
	KYCPolicyVersion string   `json:"kyc_policy_version,omitempty"`
}

// kycMissingFields reports which required identity attributes a customer
// record does not carry. It is computed on read as well as on write, because
// a record can become non-compliant without being touched: the policy changes.
func (s *Server) kycMissingFields(c *domain.Customer) []string {
	if c == nil {
		return nil
	}
	return s.policies.KYC().Missing(c.CustomerType, c.Attributes)
}

// writeCustomerWithKYC writes a customer response, attaching the identity
// gaps and -- when the policy is in warn mode and fields are missing -- the
// 299 warning that tells a client the record was accepted incomplete.
func (s *Server) writeCustomerWithKYC(w http.ResponseWriter, status int, c *domain.Customer) {
	missing := s.kycMissingFields(c)
	body := customerWithKYC{Customer: c, KYCMissingFields: missing}
	if len(missing) > 0 {
		body.KYCPolicyVersion = s.policies.KYC().Version()
		if status == http.StatusCreated || status == http.StatusOK {
			w.Header().Set("Warning", "299 - customer accepted with missing KYC fields: "+strings.Join(missing, ", "))
		}
	}
	writeJSON(w, status, body)
}

// enforceKYC decides whether a write may proceed. In the default warn mode it
// always may, and the caller reports the gap alongside the accepted record;
// an institution that has finished its data migration flips one line of YAML
// to reject and the same missing fields become a 400.
//
// Validation runs against the merged result rather than the request, because
// the case that matters most is a partial update that removes the last
// remaining value of a required field.
func (s *Server) enforceKYC(w http.ResponseWriter, c *domain.Customer) bool {
	missing := s.kycMissingFields(c)
	if len(missing) == 0 {
		return true
	}
	if s.policies.KYC().Enforce() != policy.KYCEnforcementReject {
		return true
	}
	writeErrorCode(w, http.StatusBadRequest, "validation_failed",
		"missing required identity fields for "+string(c.CustomerType)+": "+strings.Join(missing, ", "))
	return false
}
