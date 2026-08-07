package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/store"
)

func kycServer(t *testing.T, policies *policy.Set) (*Server, *store.MemoryCustomerRepo) {
	t.Helper()
	customers := store.NewMemoryCustomerRepo()
	s := New(":0", Deps{Customers: customers, Audit: store.NewMemoryAuditRepo(), Policies: policies})
	return s, customers
}

func postCustomer(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body)))
	return rec
}

type kycResponse struct {
	domain.Customer
	KYCMissingFields []string `json:"kyc_missing_fields"`
	KYCPolicyVersion string   `json:"kyc_policy_version"`
}

func decodeKYC(t *testing.T, rec *httptest.ResponseRecorder) kycResponse {
	t.Helper()
	var out kycResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

// Every customer type the API accepts must resolve to a requirement set. The
// gap this closes: corporate_foreign, trust, partnership, npo, government and
// foreign_legal_arrangement were accepted with no identity requirement at all
// while the request contract named only three types.
func TestCreateCustomerReportsMissingIdentityForEveryCustomerType(t *testing.T) {
	s, _ := kycServer(t, nil)
	kyc := policy.DefaultKYCRequiredFields()

	for i, customerType := range validCustomerTypes() {
		t.Run(string(customerType), func(t *testing.T) {
			required := kyc.Required(customerType)
			if len(required) == 0 {
				t.Fatalf("%s has no required identity fields; every accepted type must resolve to a requirement set", customerType)
			}

			bare := postCustomer(t, s, fmt.Sprintf(`{"external_id":"KYC-BARE-%d","customer_type":%q,"country_code":"JP"}`, i, customerType))
			if bare.Code != http.StatusCreated {
				t.Fatalf("create = %d, body=%s; warn enforcement must still accept the record", bare.Code, bare.Body.String())
			}
			got := decodeKYC(t, bare)
			if len(got.KYCMissingFields) != len(required) {
				t.Fatalf("missing = %v, want all %d required fields reported", got.KYCMissingFields, len(required))
			}
			if bare.Header().Get("Warning") == "" {
				t.Error("no Warning header on a record accepted with missing KYC fields")
			}
			if got.KYCPolicyVersion == "" {
				t.Error("the gap was reported without saying which policy produced it")
			}

			// The same type with every required field supplied reports nothing.
			attrs := map[string]any{}
			for _, field := range required {
				attrs[field] = "supplied"
			}
			encoded, err := json.Marshal(attrs)
			if err != nil {
				t.Fatal(err)
			}
			complete := postCustomer(t, s, fmt.Sprintf(`{"external_id":"KYC-FULL-%d","customer_type":%q,"country_code":"JP","attributes":%s}`, i, customerType, encoded))
			if complete.Code != http.StatusCreated {
				t.Fatalf("complete create = %d, body=%s", complete.Code, complete.Body.String())
			}
			if fields := decodeKYC(t, complete).KYCMissingFields; len(fields) != 0 {
				t.Fatalf("missing = %v on a complete record", fields)
			}
			if complete.Header().Get("Warning") != "" {
				t.Error("a complete record carried a warning")
			}
		})
	}
}

// A blank value is not a value: a KYC field recorded as an empty string
// carries no identity.
func TestCreateCustomerTreatsBlankIdentityAsMissing(t *testing.T) {
	s, _ := kycServer(t, nil)
	required := policy.DefaultKYCRequiredFields().Required(domain.CustomerTypeIndividual)

	attrs := map[string]any{}
	for _, field := range required {
		attrs[field] = "  "
	}
	encoded, _ := json.Marshal(attrs)
	rec := postCustomer(t, s, fmt.Sprintf(`{"external_id":"KYC-BLANK","customer_type":"individual","country_code":"JP","attributes":%s}`, encoded))
	if fields := decodeKYC(t, rec).KYCMissingFields; len(fields) != len(required) {
		t.Fatalf("missing = %v, want blank values counted as missing", fields)
	}
}

// The case a request-only check misses: an update that clears the last value
// of a required field.
func TestUpdateCustomerJudgesTheMergedRecord(t *testing.T) {
	s, _ := kycServer(t, nil)
	required := policy.DefaultKYCRequiredFields().Required(domain.CustomerTypeIndividual)
	attrs := map[string]any{}
	for _, field := range required {
		attrs[field] = "supplied"
	}
	attrs["nickname"] = "keep me"
	encoded, _ := json.Marshal(attrs)
	created := decodeKYC(t, postCustomer(t, s, fmt.Sprintf(`{"external_id":"KYC-UPDATE","customer_type":"individual","country_code":"JP","attributes":%s}`, encoded)))
	if len(created.KYCMissingFields) != 0 {
		t.Fatalf("seed record already incomplete: %v", created.KYCMissingFields)
	}

	// Clearing one required field must be reported even though the request
	// itself mentions only that field.
	body := fmt.Sprintf(`{"attributes":{%q:""},"rationale":"correcting a data entry error"}`, required[0])
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/customers/"+created.ID, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body=%s", rec.Code, rec.Body.String())
	}
	updated := decodeKYC(t, rec)
	if len(updated.KYCMissingFields) != 1 || updated.KYCMissingFields[0] != required[0] {
		t.Fatalf("missing = %v, want exactly %s", updated.KYCMissingFields, required[0])
	}
	// A partial update must not disturb attributes it never mentioned.
	if updated.Attributes["nickname"] != "keep me" {
		t.Fatalf("attributes = %v, want unmentioned values preserved", updated.Attributes)
	}
}

// The gaps are recomputed on read, because a record becomes non-compliant
// when the policy changes, not when the record is touched.
func TestGetCustomerReportsCurrentIdentityGaps(t *testing.T) {
	s, _ := kycServer(t, nil)
	created := decodeKYC(t, postCustomer(t, s, `{"external_id":"KYC-READ","customer_type":"individual","country_code":"JP"}`))

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	if fields := decodeKYC(t, rec).KYCMissingFields; len(fields) == 0 {
		t.Fatal("read did not report the identity gaps the create reported")
	}
}

// Switching the policy to reject turns the same missing fields into a refusal,
// which is the one-line migration an institution makes once its feed is clean.
func TestRejectEnforcementRefusesIncompleteIdentity(t *testing.T) {
	strict := policy.DefaultKYCRequiredFields()
	strict.Enforcement = policy.KYCEnforcementReject
	// Loaded from a file the way a deployment would, so the test exercises the
	// same path an operator's one-line YAML change takes.
	encodedPolicy, err := yaml.Marshal(strict)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "kyc_required_fields_v1.yaml")
	if err := os.WriteFile(path, encodedPolicy, 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := policy.Load(policy.Paths{KYCRequiredFields: path})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := kycServer(t, set)

	rec := postCustomer(t, s, `{"external_id":"KYC-REJECT","customer_type":"individual","country_code":"JP"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("incomplete create under reject = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "identity") {
		t.Errorf("body = %s, want the missing fields named", rec.Body.String())
	}

	attrs := map[string]any{}
	for _, field := range strict.Required(domain.CustomerTypeIndividual) {
		attrs[field] = "supplied"
	}
	encoded, _ := json.Marshal(attrs)
	ok := postCustomer(t, s, fmt.Sprintf(`{"external_id":"KYC-REJECT-OK","customer_type":"individual","country_code":"JP","attributes":%s}`, encoded))
	if ok.Code != http.StatusCreated {
		t.Fatalf("complete create under reject = %d, body=%s", ok.Code, ok.Body.String())
	}
}

// The rejection message must name every type the API actually accepts; it
// listed three of eight.
func TestInvalidCustomerTypeMessageNamesEveryAcceptedType(t *testing.T) {
	s, _ := kycServer(t, nil)
	rec := postCustomer(t, s, `{"external_id":"KYC-TYPE","customer_type":"spaceship","country_code":"JP"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	for _, customerType := range validCustomerTypes() {
		if !strings.Contains(rec.Body.String(), string(customerType)) {
			t.Errorf("message omits %q: %s", customerType, rec.Body.String())
		}
	}
}

// Kana search across several pages, including two customers sharing a name.
func TestCustomerSearchMatchesKanaAcrossPages(t *testing.T) {
	s, customers := kycServer(t, nil)
	ctx := context.Background()
	for i := range 7 {
		kana := "サノ タクマ"
		if i%3 == 2 {
			kana = "ヤマダ ハナコ"
		}
		if err := customers.Create(ctx, &domain.Customer{
			ID: fmt.Sprintf("0000000000000000000000000000k%03d", i), ExternalID: fmt.Sprintf("KANA-%d", i),
			CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive,
			Attributes: map[string]any{"name": "Takuma Sano", "name_kana": kana},
		}); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	query := "/api/v1/customers?search=" + "%E3%82%B5%E3%83%8E" + "&limit=2" // サノ
	for page := 0; page < 5; page++ {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("search = %d, body=%s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data       []domain.Customer `json:"data"`
			Pagination PaginationMeta    `json:"pagination"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, c := range body.Data {
			if c.Attributes["name_kana"] != "サノ タクマ" {
				t.Fatalf("search returned %v, which does not match the kana needle", c.Attributes["name_kana"])
			}
			seen[c.ID] = true
		}
		if !body.Pagination.HasMore {
			break
		}
		query = "/api/v1/customers?search=%E3%82%B5%E3%83%8E&limit=2&cursor=" + body.Pagination.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("kana search found %d customers, want the 5 sharing that name", len(seen))
	}
}
