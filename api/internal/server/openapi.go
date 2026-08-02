package server

import (
	"net/http"

	"github.com/ksuk/merlon/api/internal/buildinfo"
)

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, BuildOpenAPISpec())
}

// BuildOpenAPISpec constructs the Merlon HTTP API's OpenAPI 3.0.3 document.
// It is exported so that tooling outside this package (e.g.
// cmd/openapi-export) can render the spec to a file without going through an
// HTTP round trip.
func BuildOpenAPISpec() map[string]any {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Merlon AML/CFT API",
			"description": "Self-hosted AML/CFT compliance platform for Japanese non-bank financial institutions",
			// The build's own version, not a literal, so a generated client
			// records which build it was generated against.
			"version": buildinfo.Version,
		},
		// The path keys below are absolute from the server root and already
		// carry the /api/v1 prefix, so the server URL must not repeat it.
		// Declaring "/api/v1" here made every effective URL /api/v1/api/v1/...,
		// which is what a generated client would have called.
		"servers": []map[string]any{
			{"url": "/", "description": "This deployment"},
		},
		"paths": map[string]any{
			"/healthz":                                  pathProbeGET("Health check", true),
			"/healthz/live":                             pathProbeGET("Liveness probe", false),
			"/healthz/ready":                            pathProbeGET("Readiness probe", true),
			"/api/v1/customers":                         pathListCreate("Customer"),
			"/api/v1/customers/{id}":                    pathGetPut("Customer"),
			"/api/v1/customers/{id}/score":              pathPOST("Score customer risk"),
			"/api/v1/customers/{id}/screen":             pathPOST("Screen customer against sanctions lists"),
			"/api/v1/customers/{id}/scores":             pathGET("Get customer score history"),
			"/api/v1/transactions":                      pathListCreate("Transaction"),
			"/api/v1/transactions/{id}":                 pathGET("Get transaction"),
			"/api/v1/alerts":                            pathListRiskPaginated("List alerts"),
			"/api/v1/alerts/{id}":                       pathGetPatch("Alert"),
			"/api/v1/backtest":                          pathPOST("Run backtest"),
			"/api/v1/backtests":                         pathListCreate("Durable backtest job"),
			"/api/v1/backtests/{id}":                    pathGET("Get durable backtest job"),
			"/api/v1/backtests/{id}/cancel":             pathPOST("Cancel durable backtest job"),
			"/api/v1/backtests/{id}/affected-customers": pathGET("List affected backtest customers"),
			"/api/v1/reports/str":                       pathPOST("Create STR report"),
			"/api/v1/reports/str/export":                pathGET("Export STR report"),
			"/api/v1/cases":                             pathListRiskCreate("Case"),
			"/api/v1/cases/{id}":                        pathGetPatch("Case"),
			"/api/v1/cases/{id}/notes":                  pathPOST("Add case note"),
			"/api/v1/cases/{id}/related":                pathGetPost("List related cases", "Add manual related case link"),
			"/api/v1/dashboard":                         pathGET("Dashboard statistics"),
			"/api/v1/batch/score":                       pathPOST("Batch score customers"),
			"/api/v1/batch/monitor":                     pathPOST("Batch monitor transactions"),
			"/api/v1/webhooks":                          pathCRUD("Webhook", "webhooks"),
			"/api/v1/webhooks/{id}":                     pathGetDelete("Webhook"),
			"/api/v1/webhooks/{id}/deliveries":          pathGET("List webhook deliveries"),
			"/api/v1/webhooks/dlq":                      pathGET("List undelivered DLQ entries"),
			"/api/v1/webhooks/dlq/{id}/reprocess":       pathPOST("Reprocess a DLQ entry"),
			"/api/v1/admin/apikeys":                     pathCRUD("API Key", "apikeys"),
			"/api/v1/admin/apikeys/{id}":                pathDELETE("Revoke API key"),
			"/api/v1/audit":                             pathGET("List audit logs"),
			"/api/v1/audit/export":                      pathGET("Export audit logs (CSV/JSON)"),
			"/api/v1/system/config-digests":             pathGET("Get loaded configuration digests"),
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":        "http",
					"scheme":      "bearer",
					"description": "API key authentication",
				},
			},
			"schemas": map[string]any{
				"PaginationMeta": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"next_cursor": map[string]any{"type": "string"},
						"has_more":    map[string]any{"type": "boolean"},
					},
					"required": []string{"has_more"},
				},
			},
		},
		"security": []map[string]any{
			{"bearerAuth": []string{}},
		},
	}

	return spec
}

// paginationParams describes the the HTTP API contract §1.1/§1.2 query parameters shared by
// every cursor-paginated list endpoint. offset is retained but marked
// deprecated per the dual-support migration policy.
func paginationParams() []map[string]any {
	return []map[string]any{
		{
			"name":        "limit",
			"in":          "query",
			"description": "Max items per page (default 50, max 200)",
			"schema":      map[string]any{"type": "integer", "default": 50, "maximum": 200},
		},
		{
			"name":        "cursor",
			"in":          "query",
			"description": "Opaque pagination cursor from a previous response's pagination.next_cursor",
			"schema":      map[string]any{"type": "string"},
		},
		{
			"name":        "offset",
			"in":          "query",
			"description": "Deprecated: use cursor instead",
			"deprecated":  true,
			"schema":      map[string]any{"type": "integer", "default": 0},
		},
	}
}

// paginatedListResponses describes the additive {"data", "pagination"}
// envelope the HTTP API contract §1.1 specifies for list endpoint responses.
func paginatedListResponses() map[string]any {
	return map[string]any{
		"200": map[string]any{
			"description": "Success",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"data":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
							"pagination": map[string]any{"$ref": "#/components/schemas/PaginationMeta"},
						},
						"required": []string{"data", "pagination"},
					},
				},
			},
		},
		"400": map[string]any{"description": "Bad Request"},
		"401": map[string]any{"description": "Unauthorized"},
		"404": map[string]any{"description": "Not Found"},
		"429": map[string]any{"description": "Too Many Requests"},
	}
}

func pathGET(summary string) map[string]any {
	return map[string]any{
		"get": map[string]any{"summary": summary, "responses": defaultResponses()},
	}
}

func pathProbeGET(summary string, mayBeUnavailable bool) map[string]any {
	responses := map[string]any{
		"200": map[string]any{"description": "Healthy"},
	}
	if mayBeUnavailable {
		responses["503"] = map[string]any{"description": "Service Unavailable"}
	}
	return map[string]any{
		"get": map[string]any{
			"summary":   summary,
			"security":  []map[string]any{},
			"responses": responses,
		},
	}
}

func pathPOST(summary string) map[string]any {
	return map[string]any{
		"post": map[string]any{"summary": summary, "responses": defaultResponses()},
	}
}

func pathDELETE(summary string) map[string]any {
	return map[string]any{
		"delete": map[string]any{"summary": summary, "responses": defaultResponses()},
	}
}

func pathCRUD(resource, _ string) map[string]any {
	return map[string]any{
		"get":  map[string]any{"summary": "List " + resource + "s", "responses": defaultResponses()},
		"post": map[string]any{"summary": "Create " + resource, "responses": defaultResponses()},
	}
}

// pathListPaginated describes a cursor-paginated (Task 2) list-only endpoint.
func pathListPaginated(summary string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    summary,
			"parameters": paginationParams(),
			"responses":  paginatedListResponses(),
		},
	}
}

func riskQueuePaginationParams() []map[string]any {
	params := paginationParams()
	return append(params, map[string]any{
		"name":        "sort",
		"in":          "query",
		"description": "Optional queue sort; risk ranks critical > high > medium > low, with created_at/id tie-breakers",
		"schema":      map[string]any{"type": "string", "enum": []string{"risk"}},
	})
}

func pathListRiskPaginated(summary string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    summary,
			"parameters": riskQueuePaginationParams(),
			"responses":  paginatedListResponses(),
		},
	}
}

func pathListRiskCreate(resource string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "List " + resource + "s",
			"parameters": riskQueuePaginationParams(),
			"responses":  paginatedListResponses(),
		},
		"post": map[string]any{"summary": "Create " + resource, "responses": defaultResponses()},
	}
}

// pathListCreate describes a cursor-paginated (Task 2) list endpoint paired
// with a create (POST) endpoint on the same path.
func pathListCreate(resource string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "List " + resource + "s",
			"parameters": paginationParams(),
			"responses":  paginatedListResponses(),
		},
		"post": map[string]any{"summary": "Create " + resource, "responses": defaultResponses()},
	}
}

func pathGetPut(resource string) map[string]any {
	return map[string]any{
		"get": map[string]any{"summary": "Get " + resource, "responses": defaultResponses()},
		"put": map[string]any{"summary": "Update " + resource, "responses": defaultResponses()},
	}
}

func pathGetPatch(resource string) map[string]any {
	return map[string]any{
		"get":   map[string]any{"summary": "Get " + resource, "responses": defaultResponses()},
		"patch": map[string]any{"summary": "Update " + resource, "responses": defaultResponses()},
	}
}

func pathGetPost(getSummary, postSummary string) map[string]any {
	return map[string]any{
		"get":  map[string]any{"summary": getSummary, "responses": defaultResponses()},
		"post": map[string]any{"summary": postSummary, "responses": defaultResponses()},
	}
}

func pathGetDelete(resource string) map[string]any {
	return map[string]any{
		"get":    map[string]any{"summary": "Get " + resource, "responses": defaultResponses()},
		"delete": map[string]any{"summary": "Delete " + resource, "responses": defaultResponses()},
	}
}

func defaultResponses() map[string]any {
	return map[string]any{
		"200": map[string]any{"description": "Success"},
		"400": map[string]any{"description": "Bad Request"},
		"401": map[string]any{"description": "Unauthorized"},
		"404": map[string]any{"description": "Not Found"},
		"429": map[string]any{"description": "Too Many Requests"},
	}
}
