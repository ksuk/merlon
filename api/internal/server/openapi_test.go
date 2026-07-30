package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
