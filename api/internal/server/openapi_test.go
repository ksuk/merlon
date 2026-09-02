package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/buildinfo"
)

func fetchOpenAPISpec(t *testing.T) map[string]any {
	t.Helper()
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var spec map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&spec); err != nil {
		t.Fatalf("decode openapi spec: %v", err)
	}
	return spec
}

// previousPaths pins the path set the OpenAPI document exposed before Task 3
// (cursor pagination params/response schema) touched it. None of these may
// be removed by an additive change.
var previousPaths = []string{
	"/healthz",
	"/api/v1/customers",
	"/api/v1/customers/{id}",
	"/api/v1/customers/{id}/score",
	"/api/v1/customers/{id}/screen",
	"/api/v1/customers/{id}/scores",
	"/api/v1/transactions",
	"/api/v1/transactions/{id}",
	"/api/v1/alerts",
	"/api/v1/alerts/{id}",
	"/api/v1/backtest",
	"/api/v1/reports/str",
	"/api/v1/reports/str/export",
	"/api/v1/cases",
	"/api/v1/cases/{id}",
	"/api/v1/cases/{id}/notes",
	"/api/v1/dashboard",
	"/api/v1/batch/score",
	"/api/v1/batch/monitor",
	"/api/v1/webhooks",
	"/api/v1/webhooks/{id}",
	"/api/v1/webhooks/{id}/deliveries",
	"/api/v1/admin/apikeys",
	"/api/v1/admin/apikeys/{id}",
	"/api/v1/audit",
}

// previousListResponseStatuses pins the response status codes a list
// endpoint's GET operation exposed before Task 3.
var previousListResponseStatuses = []string{"200", "400", "401", "404", "429"}

func TestOpenAPI_ExistingFieldsPreserved(t *testing.T) {
	spec := fetchOpenAPISpec(t)

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec.paths missing or not an object")
	}

	for _, p := range previousPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("path %q missing from openapi spec", p)
		}
	}

	alertsPath, ok := paths["/api/v1/alerts"].(map[string]any)
	if !ok {
		t.Fatal("/api/v1/alerts missing from spec")
	}
	get, ok := alertsPath["get"].(map[string]any)
	if !ok {
		t.Fatal("/api/v1/alerts get operation missing")
	}
	responses, ok := get["responses"].(map[string]any)
	if !ok {
		t.Fatal("/api/v1/alerts get.responses missing")
	}
	for _, status := range previousListResponseStatuses {
		if _, ok := responses[status]; !ok {
			t.Errorf("response status %q missing from /api/v1/alerts GET", status)
		}
	}

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec.components missing")
	}
	if _, ok := components["securitySchemes"]; !ok {
		t.Error("components.securitySchemes missing")
	}
}

// TestBuildOpenAPISpec_MatchesHandlerOutput guards the refactor that
// extracted the spec construction out of handleOpenAPI into the exported
// BuildOpenAPISpec, which cmd/openapi-export calls directly (without an HTTP
// round trip). It reuses previousPaths rather than duplicating the path
// list, so this stays in sync with TestOpenAPI_ExistingFieldsPreserved.
func TestBuildOpenAPISpec_MatchesHandlerOutput(t *testing.T) {
	spec := BuildOpenAPISpec()

	if spec["openapi"] != "3.0.3" {
		t.Errorf(`spec["openapi"] = %v, want "3.0.3"`, spec["openapi"])
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec.paths missing or not an object")
	}
	for _, p := range previousPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("path %q missing from BuildOpenAPISpec() output", p)
		}
	}
}

func TestBuildOpenAPISpec_UsesBuildVersion(t *testing.T) {
	previous := buildinfo.Version
	buildinfo.Version = "v9.8.7-test"
	t.Cleanup(func() {
		buildinfo.Version = previous
	})

	spec := BuildOpenAPISpec()
	info, ok := spec["info"].(map[string]any)
	if !ok {
		t.Fatal("spec.info missing or not an object")
	}
	if got := info["version"]; got != "v9.8.7-test" {
		t.Errorf("info.version = %v, want v9.8.7-test", got)
	}
}

// TestOpenAPISpecDocumentsHealthProbes covers the liveness and readiness
// probes, which the server registers but the spec did not describe, so a
// generated client had no way to know they exist.
func TestOpenAPISpecDocumentsHealthProbes(t *testing.T) {
	spec := fetchOpenAPISpec(t)

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec.paths missing or not an object")
	}

	for _, p := range []string{"/healthz", "/healthz/live", "/healthz/ready"} {
		pathItem, ok := paths[p].(map[string]any)
		if !ok {
			t.Errorf("path %q missing from openapi spec", p)
			continue
		}
		get, ok := pathItem["get"].(map[string]any)
		if !ok {
			t.Errorf("%s get operation missing", p)
			continue
		}

		security, ok := get["security"].([]any)
		if !ok {
			t.Errorf("%s get.security missing or not an array", p)
		} else if len(security) != 0 {
			t.Errorf("%s get.security = %v, want [] (probe must not require authentication)", p, security)
		}
	}

	for _, p := range []string{"/healthz", "/healthz/ready"} {
		get := paths[p].(map[string]any)["get"].(map[string]any)
		responses, ok := get["responses"].(map[string]any)
		if !ok {
			t.Errorf("%s get.responses missing", p)
			continue
		}
		if _, ok := responses["503"]; !ok {
			t.Errorf("%s get.responses missing 503 Service Unavailable", p)
		}
	}

	live := paths["/healthz/live"].(map[string]any)["get"].(map[string]any)
	liveResponses := live["responses"].(map[string]any)
	if _, ok := liveResponses["503"]; ok {
		t.Error("/healthz/live documents 503, but liveness does not check dependencies")
	}
}

func TestOpenAPISpecDocumentsTMContractAndTransactionType(t *testing.T) {
	spec := fetchOpenAPISpec(t)
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)

	tmContract := schemas["TMContract"].(map[string]any)
	tmProperties := tmContract["properties"].(map[string]any)
	for _, field := range []string{"contract_version", "supported_detectors", "compatibility_warnings", "default_digest"} {
		if _, ok := tmProperties[field]; !ok {
			t.Errorf("TMContract.properties.%s missing", field)
		}
	}

	status := schemas["SystemStatus"].(map[string]any)
	statusProperties := status["properties"].(map[string]any)
	if _, ok := statusProperties["tm_contract"]; !ok {
		t.Error("SystemStatus.properties.tm_contract missing")
	}

	transaction := schemas["Transaction"].(map[string]any)
	transactionProperties := transaction["properties"].(map[string]any)
	if _, ok := transactionProperties["transaction_type"]; !ok {
		t.Error("Transaction.properties.transaction_type missing")
	}
	create := schemas["CreateTransactionRequest"].(map[string]any)
	createProperties := create["properties"].(map[string]any)
	if _, ok := createProperties["transaction_type"]; !ok {
		t.Error("CreateTransactionRequest.properties.transaction_type missing")
	}
}

func TestOpenAPISpecDocumentsTransactionMonitoringEvaluation(t *testing.T) {
	spec := fetchOpenAPISpec(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	transaction := schemas["Transaction"].(map[string]any)
	properties := transaction["properties"].(map[string]any)
	monitoring, ok := properties["monitoring_evaluation"].(map[string]any)
	if !ok {
		t.Fatal("Transaction.properties.monitoring_evaluation missing")
	}
	if monitoring["$ref"] != "#/components/schemas/TransactionMonitoringEvaluation" {
		t.Fatalf("monitoring_evaluation schema = %#v", monitoring)
	}

	evaluation := schemas["TransactionMonitoringEvaluation"].(map[string]any)
	evaluationProperties := evaluation["properties"].(map[string]any)
	for _, field := range []string{"pending_evaluation_id", "status", "reason"} {
		if _, ok := evaluationProperties[field]; !ok {
			t.Errorf("TransactionMonitoringEvaluation.properties.%s missing", field)
		}
	}
}

func TestOpenAPI_PaginationFieldsPresent(t *testing.T) {
	spec := fetchOpenAPISpec(t)

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec.components missing")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("components.schemas missing")
	}
	paginationMeta, ok := schemas["PaginationMeta"].(map[string]any)
	if !ok {
		t.Fatal("components.schemas.PaginationMeta missing")
	}
	props, ok := paginationMeta["properties"].(map[string]any)
	if !ok {
		t.Fatal("PaginationMeta.properties missing")
	}
	if _, ok := props["next_cursor"]; !ok {
		t.Error("PaginationMeta.properties.next_cursor missing")
	}
	if _, ok := props["has_more"]; !ok {
		t.Error("PaginationMeta.properties.has_more missing")
	}

	paths := spec["paths"].(map[string]any)
	listEndpoints := []string{"/api/v1/customers", "/api/v1/transactions", "/api/v1/alerts", "/api/v1/cases"}

	for _, path := range listEndpoints {
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("%s missing from spec", path)
		}
		get, ok := pathItem["get"].(map[string]any)
		if !ok {
			t.Fatalf("%s get operation missing", path)
		}

		responses, ok := get["responses"].(map[string]any)
		if !ok {
			t.Fatalf("%s get.responses missing", path)
		}
		ok200, ok := responses["200"].(map[string]any)
		if !ok {
			t.Fatalf("%s get.responses.200 missing", path)
		}
		content, ok := ok200["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s get.responses.200.content missing", path)
		}
		appJSON, ok := content["application/json"].(map[string]any)
		if !ok {
			t.Fatalf("%s get.responses.200.content.application/json missing", path)
		}
		schema, ok := appJSON["schema"].(map[string]any)
		if !ok {
			t.Fatalf("%s response schema missing", path)
		}
		if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/components/schemas/") {
			components := spec["components"].(map[string]any)["schemas"].(map[string]any)
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			resolved, exists := components[name].(map[string]any)
			if !exists {
				t.Fatalf("%s response schema reference %q is missing", path, ref)
			}
			schema = resolved
		}
		schemaProps, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s response schema.properties missing", path)
		}
		pagination, ok := schemaProps["pagination"].(map[string]any)
		if !ok {
			t.Fatalf("%s response schema.properties.pagination missing", path)
		}
		if pagination["$ref"] != "#/components/schemas/PaginationMeta" {
			t.Errorf("%s response pagination field does not reference PaginationMeta: %v", path, pagination)
		}

		// offset must be present (dual support) and marked deprecated.
		parameters, ok := get["parameters"].([]any)
		if !ok {
			t.Fatalf("%s get.parameters missing", path)
		}
		var foundDeprecatedOffset bool
		for _, raw := range parameters {
			param, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if param["name"] == "offset" && param["deprecated"] == true {
				foundDeprecatedOffset = true
			}
		}
		if !foundDeprecatedOffset {
			t.Errorf("%s missing a deprecated offset parameter", path)
		}
	}
}

func TestOpenAPI_TransactionListRequiresCustomerID(t *testing.T) {
	spec := fetchOpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	transactions := paths["/api/v1/transactions"].(map[string]any)
	get := transactions["get"].(map[string]any)
	params := get["parameters"].([]any)

	for _, raw := range params {
		param := raw.(map[string]any)
		if param["name"] != "customer_id" {
			continue
		}
		if param["in"] != "query" || param["required"] != true {
			t.Fatalf("customer_id parameter = %#v, want required query parameter", param)
		}
		schema := param["schema"].(map[string]any)
		if schema["type"] != "string" {
			t.Fatalf("customer_id schema = %#v, want string", schema)
		}
		return
	}
	t.Fatal("GET /api/v1/transactions customer_id parameter missing")
}

func TestWave2OpenAPISemanticParity(t *testing.T) {
	spec := fetchOpenAPISpec(t)
	paths := spec["paths"].(map[string]any)

	operation := func(path, method string) map[string]any {
		t.Helper()
		item, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI path %s is missing", path)
		}
		op, ok := item[method].(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI operation %s %s is missing", method, path)
		}
		return op
	}
	responses := func(path, method string) map[string]any {
		op := operation(path, method)
		result, ok := op["responses"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s responses are missing", method, path)
		}
		return result
	}
	for _, route := range []struct {
		path, method string
		statuses     []string
	}{
		{"/api/v1/reports/str", "get", []string{"200", "400", "401", "429", "500", "503"}},
		{"/api/v1/reports/str", "post", []string{"201", "400", "401", "404", "409", "422", "500", "503"}},
		{"/api/v1/reports/str/{}", "get", []string{"200", "401", "404", "500", "503"}},
		{"/api/v1/reports/str/{}", "put", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/reports/str/{}", "patch", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/reports/str/{}/submit", "post", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/reports/str/export", "get", []string{"200", "400", "401", "404", "422", "500", "503"}},
		{"/api/v1/cases", "get", []string{"200", "400", "401", "429", "500", "503"}},
		{"/api/v1/cases", "post", []string{"201", "400", "401", "403", "404", "409", "500", "503"}},
		{"/api/v1/cases/{}", "get", []string{"200", "401", "404", "500", "503"}},
		{"/api/v1/cases/{}", "patch", []string{"200", "400", "401", "403", "404", "409", "500", "503"}},
		{"/api/v1/cases/{}/timeline", "get", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/cases/{}/export", "get", []string{"200", "401", "404", "500", "503"}},
		{"/api/v1/cases/{}/evidence", "post", []string{"201", "400", "401", "404", "500", "503"}},
		{"/api/v1/cases/{id}/evidence/{evidence}/corrections", "post", []string{"201", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/cases/{id}/checklist/{item}", "put", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/cases/{}/work-items", "post", []string{"201", "400", "401", "404", "500", "503"}},
		{"/api/v1/cases/{id}/work-items/{item}", "patch", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/cases/{}/related", "get", []string{"200", "401", "404", "500", "503"}},
		{"/api/v1/cases/{}/related", "post", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/cases/{id}/related/{relationship}", "put", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/cases/{id}/related/{relationship}", "delete", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/alerts/{}/decisions", "get", []string{"200", "401", "404", "500", "503"}},
		{"/api/v1/alerts/{}", "patch", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/alerts/bulk-close", "post", []string{"200", "400", "401", "409", "500", "503"}},
		{"/api/v1/alerts/bulk-case", "post", []string{"200", "400", "401", "404", "409", "500", "503"}},
		{"/api/v1/webhooks/inbound/customer-status", "post", []string{"200", "400", "401", "404", "500", "503"}},
		{"/api/v1/operators", "get", []string{"200", "401", "500", "503"}},
		{"/api/v1/audit", "get", []string{"200", "400", "401", "403", "429", "500", "503"}},
		{"/api/v1/audit/export", "get", []string{"200", "400", "401", "403", "404", "422", "500", "503"}},
	} {
		got := responses(strings.ReplaceAll(route.path, "{}", "{id}"), route.method)
		for _, status := range route.statuses {
			if _, ok := got[status]; !ok {
				t.Errorf("%s %s missing documented response %s", route.method, route.path, status)
			}
		}
		if _, ok := got["200"]; ok && route.path != "/api/v1/reports/str/export" && route.path != "/api/v1/audit/export" {
			response := got["200"].(map[string]any)
			content, ok := response["content"].(map[string]any)
			if !ok || content["application/json"] == nil {
				t.Errorf("%s %s success response has no JSON schema", route.method, route.path)
			}
		}
	}

	alertList := responses("/api/v1/alerts", "get")["200"].(map[string]any)
	alertListSchema := alertList["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	alertItems := alertListSchema["properties"].(map[string]any)["data"].(map[string]any)["items"].(map[string]any)
	if alertItems["$ref"] != "#/components/schemas/Alert" {
		t.Fatalf("GET /api/v1/alerts data items = %#v, want Alert schema", alertItems)
	}
	alertPatchBody := operation("/api/v1/alerts/{id}", "patch")["requestBody"].(map[string]any)
	if alertPatchBody["required"] != true {
		t.Fatal("PATCH /api/v1/alerts/{id} request body must be required")
	}
	alertPatchSchema := alertPatchBody["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if alertPatchSchema["$ref"] != "#/components/schemas/AlertUpdateRequest" {
		t.Fatalf("PATCH /api/v1/alerts/{id} request schema = %#v", alertPatchSchema)
	}
	componentSchemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	alertRequestProperties := componentSchemas["AlertUpdateRequest"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"status", "resolved_by", "rationale", "confirm", "assigned_to", "assigned_team", "due_at", "clear_due_at", "expected_updated_at"} {
		if _, ok := alertRequestProperties[field]; !ok {
			t.Errorf("AlertUpdateRequest is missing handler field %q", field)
		}
	}

	requestFields := func(path, method string, fields ...string) {
		t.Helper()
		op := operation(path, method)
		body, ok := op["requestBody"].(map[string]any)
		if !ok || body["required"] != true {
			t.Fatalf("%s %s request body is not required/documented", method, path)
		}
		content := body["content"].(map[string]any)
		schema := content["application/json"].(map[string]any)["schema"].(map[string]any)
		required, ok := schema["required"].([]any)
		if !ok {
			t.Fatalf("%s %s request body required fields are missing", method, path)
		}
		seen := map[string]bool{}
		for _, raw := range required {
			if field, ok := raw.(string); ok {
				seen[field] = true
			}
		}
		for _, field := range fields {
			if !seen[field] {
				t.Errorf("%s %s required request field %q is missing", method, path, field)
			}
		}
	}
	requestFields("/api/v1/reports/str", "post", "alert_id", "case_id", "suspicious_point")
	requestFields("/api/v1/reports/str/{id}/submit", "post", "submission_evidence")
	requestFields("/api/v1/cases", "post", "customer_id", "summary")
	requestFields("/api/v1/cases/{id}/notes", "post", "content")
	requestFields("/api/v1/cases/{id}/evidence", "post", "description", "source", "evidence_type", "collected_by")
	requestFields("/api/v1/cases/{id}/evidence/{evidence}/corrections", "post", "reason")
	requestFields("/api/v1/cases/{id}/checklist/{item}", "put", "label", "completed")
	requestFields("/api/v1/cases/{id}/work-items", "post", "title")
	requestFields("/api/v1/cases/{id}/related", "post", "related_case_id", "rationale")
	requestFields("/api/v1/cases/{id}/related/{relationship}", "put", "rationale")
	requestFields("/api/v1/cases/{id}/related/{relationship}", "delete", "reason")
	requestFields("/api/v1/alerts/bulk-close", "post", "reason")
	requestFields("/api/v1/alerts/bulk-case", "post", "alert_ids")
	requestFields("/api/v1/webhooks/inbound/customer-status", "post", "external_id", "status")

	exportContent := func(path string) {
		responses := responses(path, "get")
		success := responses["200"].(map[string]any)
		content := success["content"].(map[string]any)
		if content["text/csv"] == nil || content["application/json"] == nil {
			t.Errorf("%s export must document both CSV and JSON content types", path)
		}
		headers, ok := success["headers"].(map[string]any)
		if !ok || headers["Content-Disposition"] == nil {
			t.Errorf("%s export must document Content-Disposition", path)
		}
	}
	exportContent("/api/v1/reports/str/export")
	exportContent("/api/v1/audit/export")
}

func TestOpenAPI_RegisteredCompatibilityRoutesHaveTypedContracts(t *testing.T) {
	spec := fetchOpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	type expectation struct {
		path     string
		method   string
		statuses []string
		body     bool
	}
	expectations := []expectation{
		{"/api/v1/accounts", "post", []string{"201", "400", "500"}, true},
		{"/api/v1/accounts/{id}", "get", []string{"200", "404", "500"}, false},
		{"/api/v1/accounts/{id}/customers", "get", []string{"200", "404", "500"}, false},
		{"/api/v1/accounts/{id}/customers", "post", []string{"201", "400", "404", "500"}, true},
		{"/api/v1/admin/retention-policies", "get", []string{"200", "500", "503"}, false},
		{"/api/v1/admin/retention-policies/{category}", "put", []string{"200", "400", "404", "500", "503"}, true},
		{"/api/v1/admin/users", "get", []string{"200", "500", "503"}, false},
		{"/api/v1/auth/login", "post", []string{"200", "400", "401", "500", "503"}, true},
		{"/api/v1/auth/logout", "post", []string{"200"}, false},
		{"/api/v1/auth/refresh", "post", []string{"200", "401", "500", "503"}, false},
		{"/api/v1/auth/me", "get", []string{"200", "401", "500", "503"}, false},
		{"/api/v1/config/validate", "post", []string{"200", "400", "500", "503"}, true},
		{"/api/v1/openapi.json", "get", []string{"200"}, false},
		{"/api/v1/rules", "get", []string{"200", "400", "500", "503"}, false},
		{"/api/v1/rules", "post", []string{"201", "400", "500", "503"}, true},
		{"/api/v1/rules/{id}", "get", []string{"200", "400", "404", "500", "503"}, false},
		{"/api/v1/rules/{id}", "put", []string{"200", "400", "404", "500", "503"}, true},
		{"/api/v1/rules/{id}/export", "get", []string{"200", "400", "404", "500", "503"}, false},
		{"/api/v1/rules/{id}/activate", "post", []string{"200", "403", "404", "500", "503"}, false},
		{"/api/v1/rules/{id}/deactivate", "post", []string{"200", "403", "404", "500", "503"}, false},
		{"/api/v1/rules/import", "post", []string{"201", "400", "409", "500", "503"}, true},
		{"/api/v1/screening/check", "post", []string{"200", "400", "404", "500", "503"}, true},
		{"/api/v1/screening/results/{id}", "patch", []string{"200", "400", "404", "500", "503"}, true},
		{"/api/v1/setup", "post", []string{"201", "400", "409", "500", "503"}, true},
		{"/api/v1/system/info", "get", []string{"200"}, false},
		{"/api/v1/whitelist", "get", []string{"200", "500", "503"}, false},
		{"/api/v1/whitelist", "post", []string{"201", "400", "404", "500", "503"}, true},
		{"/api/v1/whitelist/{id}", "get", []string{"200", "404", "500", "503"}, false},
		{"/api/v1/whitelist/{id}/approve", "post", []string{"200", "403", "404", "409", "500", "503"}, false},
		{"/api/v1/whitelist/{id}/reviews", "post", []string{"201", "400", "403", "404", "409", "500", "503"}, true},
		{"/api/v1/whitelist/{id}/revoke", "post", []string{"200", "403", "404", "409", "500", "503"}, false},
	}
	for _, want := range expectations {
		item, ok := paths[want.path].(map[string]any)
		if !ok {
			t.Errorf("OpenAPI path %s missing", want.path)
			continue
		}
		op, ok := item[want.method].(map[string]any)
		if !ok {
			t.Errorf("OpenAPI operation %s %s missing", want.method, want.path)
			continue
		}
		responses, ok := op["responses"].(map[string]any)
		if !ok || len(responses) < 1 {
			t.Errorf("%s %s has no responses", want.method, want.path)
			continue
		}
		if _, generic := responses["default"]; generic {
			t.Errorf("%s %s retains a generic default response", want.method, want.path)
		}
		for _, status := range want.statuses {
			if _, present := responses[status]; !present {
				t.Errorf("%s %s missing response %s", want.method, want.path, status)
			}
		}
		if want.body {
			body, ok := op["requestBody"].(map[string]any)
			if !ok || body["required"] != true {
				t.Errorf("%s %s request body is not required", want.method, want.path)
			}
		}
	}

	ruleExport := paths["/api/v1/rules/{id}/export"].(map[string]any)["get"].(map[string]any)
	exportContent := ruleExport["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	if exportContent["application/json"] == nil || exportContent["application/x-yaml"] == nil {
		t.Fatalf("rule export content types = %#v, want JSON and YAML", exportContent)
	}
}

func TestOpenAPI_WebhookContractsDescribeDurableRetryState(t *testing.T) {
	spec := fetchOpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	operation := func(path, method string) map[string]any {
		t.Helper()
		return paths[path].(map[string]any)[method].(map[string]any)
	}
	responses := func(path, method string) map[string]any {
		t.Helper()
		return operation(path, method)["responses"].(map[string]any)
	}
	for _, route := range []struct {
		path, method, status string
	}{
		{"/api/v1/webhooks", "get", "200"},
		{"/api/v1/webhooks", "post", "201"},
		{"/api/v1/webhooks/{id}", "get", "200"},
		{"/api/v1/webhooks/{id}", "delete", "200"},
		{"/api/v1/webhooks/{id}/deliveries", "get", "200"},
		{"/api/v1/webhooks/dlq", "get", "200"},
		{"/api/v1/webhooks/dlq/{id}/reprocess", "post", "200"},
	} {
		response := responses(route.path, route.method)[route.status].(map[string]any)
		if response["content"] == nil {
			t.Errorf("%s %s success response has no schema", route.method, route.path)
		}
	}
	create := operation("/api/v1/webhooks", "post")
	body := create["requestBody"].(map[string]any)
	if body["required"] != true {
		t.Fatal("POST /api/v1/webhooks request body must be required")
	}
	schema := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	allOf := schema["allOf"].([]any)
	if len(allOf) != 1 || allOf[0].(map[string]any)["$ref"] != "#/components/schemas/WebhookCreateRequest" {
		t.Fatalf("webhook create request schema = %#v", schema)
	}
	components := spec["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"Webhook", "WebhookDelivery", "DLQEntry", "WebhookReprocessResponse"} {
		if components[name] == nil {
			t.Errorf("missing webhook schema %q", name)
		}
	}
}
