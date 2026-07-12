package server

import "net/http"

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
			"version":     "1.0.0",
		},
		"servers": []map[string]any{
			{"url": "/api/v1", "description": "API v1"},
		},
		"paths": map[string]any{
			"/healthz":                            pathGET("Health check"),
			"/api/v1/customers":                   pathListCreate("Customer"),
			"/api/v1/customers/{id}":              pathGetPut("Customer"),
			"/api/v1/customers/{id}/score":        pathPOST("Score customer risk"),
			"/api/v1/customers/{id}/screen":       pathPOST("Screen customer against sanctions lists"),
			"/api/v1/customers/{id}/scores":       pathGET("Get customer score history"),
			"/api/v1/transactions":                pathListCreate("Transaction"),
			"/api/v1/transactions/{id}":           pathGET("Get transaction"),
			"/api/v1/alerts":                      pathListPaginated("List alerts"),
			"/api/v1/alerts/{id}":                 pathGetPatch("Alert"),
			"/api/v1/backtest":                    pathPOST("Run backtest"),
			"/api/v1/reports/str":                 pathPOST("Create STR report"),
			"/api/v1/reports/str/export":          pathGET("Export STR report"),
			"/api/v1/cases":                       pathListCreate("Case"),
			"/api/v1/cases/{id}":                  pathGetPatch("Case"),
			"/api/v1/cases/{id}/notes":            pathPOST("Add case note"),
			"/api/v1/cases/{id}/related":          pathGetPost("List related cases", "Add manual related case link"),
			"/api/v1/dashboard":                   pathGET("Dashboard statistics"),
			"/api/v1/batch/score":                 pathPOST("Batch score customers"),
			"/api/v1/batch/monitor":               pathPOST("Batch monitor transactions"),
			"/api/v1/webhooks":                    pathCRUD("Webhook", "webhooks"),
			"/api/v1/webhooks/{id}":               pathGetDelete("Webhook"),
			"/api/v1/webhooks/{id}/deliveries":    pathGET("List webhook deliveries"),
			"/api/v1/webhooks/dlq":                pathGET("List undelivered DLQ entries"),
			"/api/v1/webhooks/dlq/{id}/reprocess": pathPOST("Reprocess a DLQ entry"),
			"/api/v1/admin/apikeys":               pathCRUD("API Key", "apikeys"),
			"/api/v1/admin/apikeys/{id}":          pathDELETE("Revoke API key"),
			"/api/v1/audit":                       pathGET("List audit logs"),
			"/api/v1/audit/export":                pathGET("Export audit logs (CSV/JSON)"),
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
