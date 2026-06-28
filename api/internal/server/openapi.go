package server

import "net/http"

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
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
			"/healthz":                         pathGET("Health check"),
			"/api/v1/customers":                pathCRUD("Customer", "customers"),
			"/api/v1/customers/{id}":            pathGetPut("Customer"),
			"/api/v1/customers/{id}/score":      pathPOST("Score customer risk"),
			"/api/v1/customers/{id}/screen":     pathPOST("Screen customer against sanctions lists"),
			"/api/v1/customers/{id}/scores":     pathGET("Get customer score history"),
			"/api/v1/transactions":              pathCRUD("Transaction", "transactions"),
			"/api/v1/transactions/{id}":         pathGET("Get transaction"),
			"/api/v1/alerts":                    pathGET("List alerts"),
			"/api/v1/alerts/{id}":               pathGetPatch("Alert"),
			"/api/v1/backtest":                  pathPOST("Run backtest"),
			"/api/v1/reports/str":               pathPOST("Create STR report"),
			"/api/v1/reports/str/export":        pathGET("Export STR report"),
			"/api/v1/cases":                     pathCRUD("Case", "cases"),
			"/api/v1/cases/{id}":                pathGetPatch("Case"),
			"/api/v1/cases/{id}/notes":          pathPOST("Add case note"),
			"/api/v1/dashboard":                 pathGET("Dashboard statistics"),
			"/api/v1/batch/score":               pathPOST("Batch score customers"),
			"/api/v1/batch/monitor":             pathPOST("Batch monitor transactions"),
			"/api/v1/webhooks":                  pathCRUD("Webhook", "webhooks"),
			"/api/v1/webhooks/{id}":             pathGetDelete("Webhook"),
			"/api/v1/webhooks/{id}/deliveries":  pathGET("List webhook deliveries"),
			"/api/v1/admin/apikeys":             pathCRUD("API Key", "apikeys"),
			"/api/v1/admin/apikeys/{id}":        pathDELETE("Revoke API key"),
			"/api/v1/audit":                     pathGET("List audit logs"),
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"description":  "API key authentication",
				},
			},
		},
		"security": []map[string]any{
			{"bearerAuth": []string{}},
		},
	}

	writeJSON(w, http.StatusOK, spec)
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
