package server

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
)

// assessTravelRule computes the server's own Travel Rule verdict for a
// transaction and records it.
//
// The applicability flag was previously whatever the client asserted, and a
// transaction with no counterparty block left it nil forever -- rendered as
// "legacy" in the UI, indistinguishable from a row that predated the field.
// Every transaction is now assessed.
//
// Under the default assertion_authority: client the client's own claim is
// never overwritten. Both are kept and a disagreement is recorded as a
// finding, which is the honest outcome: the institution asserted one thing,
// the configured policy implies another, and a reviewer needs to see both.
// Switching the policy to server authority is what turns that into a refusal.
func (s *Server) assessTravelRule(w http.ResponseWriter, t *domain.Transaction) (policy.Assessment, bool) {
	travelRule := s.policies.TravelRule()
	assessment := travelRule.Assess(t, s.travelRuleBaseAmount(t, travelRule.BaseCurrency()), time.Now().UTC())

	// A client that claims the rule applies where the policy says it does not
	// (or the reverse) is not corrected silently.
	if t.TravelRuleApplicable != nil && *t.TravelRuleApplicable != assessment.Applicable {
		assessment.Conflict = true
	}

	counterpartyType := domain.CounterpartyTypeUnknown
	if t.Counterparty != nil && t.Counterparty.CounterpartyType != "" {
		counterpartyType = t.Counterparty.CounterpartyType
	}

	// A transaction declared complete while required evidence is missing is
	// self-contradictory data. Rejecting it is not a new restriction on
	// anything that was previously coherent.
	if t.Counterparty != nil && t.Counterparty.TravelRuleStatus == domain.TravelRuleComplete && assessment.Applicable {
		if missing := travelRule.MissingEvidence(counterpartyType, t); len(missing) > 0 {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed,
				"travel_rule_status is complete but required evidence is missing: "+strings.Join(missing, ", "))
			return assessment, false
		}
	}

	// A free-text reason with no code maps to `other` rather than being
	// rejected: the existing field keeps working, and the closed enum starts
	// carrying the answerable version of the same statement.
	if code := strings.TrimSpace(t.TravelRuleNotApplicableReasonCode); code != "" {
		if !travelRule.ValidReasonCode(code) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed,
				"travel_rule_not_applicable_reason_code must be one of: "+strings.Join(travelRule.ReasonCodes(), ", "))
			return assessment, false
		}
	} else if strings.TrimSpace(t.TravelRuleNotApplicableReason) != "" {
		t.TravelRuleNotApplicableReasonCode = policy.TravelRuleReasonOther
	}

	t.TravelRuleStatus = travelRuleStatusFrom(t, assessment)
	encoded, err := json.Marshal(assessment)
	if err == nil {
		var stored map[string]any
		if json.Unmarshal(encoded, &stored) == nil {
			t.TravelRuleAssessment = stored
		}
	}
	if assessment.Conflict {
		w.Header().Set("Warning", "299 - travel_rule_applicable conflicts with the policy assessment")
	}
	return assessment, true
}

// travelRuleBaseAmount converts a transaction's value into the policy's
// threshold currency. There is no FX table in this system and inventing one
// here would be worse than not converting: a wrong rate produces a confident
// wrong answer. So a matching currency is used as-is, and a differing one is
// treated as at or above the threshold. That is the Fail-Alert reading -- a
// transaction whose value cannot be compared is assessed as if it counts,
// leaving a reviewer to see it rather than leaving it silently exempt.
func (s *Server) travelRuleBaseAmount(t *domain.Transaction, thresholdCurrency string) float64 {
	if t == nil {
		return 0
	}
	if strings.EqualFold(t.Currency, thresholdCurrency) {
		return t.Amount
	}
	if s.tmBaseCurrency != "" && strings.EqualFold(t.Currency, s.tmBaseCurrency) && strings.EqualFold(s.tmBaseCurrency, thresholdCurrency) {
		return t.Amount
	}
	return math.Inf(1)
}

// travelRuleStatusFrom derives the server's status. It is deliberately
// independent of the client's counterparty.travel_rule_status: that field
// records what the submitting system believed, this one records what the
// policy concludes from the evidence actually present.
func travelRuleStatusFrom(t *domain.Transaction, assessment policy.Assessment) string {
	if !assessment.Applicable {
		return string(domain.TravelRuleNotApplicable)
	}
	if len(assessment.MissingFields) > 0 {
		return string(domain.TravelRuleIncomplete)
	}
	return string(domain.TravelRuleComplete)
}
