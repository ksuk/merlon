package server

import (
	"net/http"
	"strings"

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
	schemas := map[string]any{
		"PaginationMeta": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"next_cursor": map[string]any{"type": "string"},
				"has_more":    map[string]any{"type": "boolean"},
			},
			"required": []string{"has_more"},
		},
	}
	for name, schema := range wave2Schemas() {
		schemas[name] = schema
	}
	for name, schema := range compatibilitySchemas() {
		schemas[name] = schema
	}
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
			"/healthz":                                           pathProbeGET("Health check", true),
			"/healthz/live":                                      pathProbeGET("Liveness probe", false),
			"/healthz/ready":                                     pathProbeGET("Readiness probe", true),
			"/api/v1/customers":                                  pathListCreate("Customer"),
			"/api/v1/customers/{id}":                             pathGetPut("Customer"),
			"/api/v1/customers/{id}/score":                       pathScoreCustomer(),
			"/api/v1/customers/{id}/screen":                      pathScreenCustomer(),
			"/api/v1/customers/{id}/scores":                      pathScoreHistory(),
			"/api/v1/transactions":                               pathTransactionListCreate(),
			"/api/v1/transactions/{id}":                          pathGET("Get transaction"),
			"/api/v1/alerts":                                     pathListRiskPaginated("List alerts"),
			"/api/v1/alerts/{id}":                                pathAlert(),
			"/api/v1/alerts/bulk-close":                          pathBulkCloseAlerts(),
			"/api/v1/alerts/bulk-case":                           pathBulkCaseAssignment(),
			"/api/v1/alerts/{id}/decisions":                      pathAlertDecisionHistory(),
			"/api/v1/backtest":                                   pathPOST("Run backtest"),
			"/api/v1/backtests":                                  pathListCreate("Durable backtest job"),
			"/api/v1/backtests/{id}":                             pathGET("Get durable backtest job"),
			"/api/v1/backtests/{id}/cancel":                      pathPOST("Cancel durable backtest job"),
			"/api/v1/backtests/{id}/affected-customers":          pathGET("List affected backtest customers"),
			"/api/v1/reports/str":                                pathSTRReports(),
			"/api/v1/reports/str/{id}":                           pathSTRReport(),
			"/api/v1/reports/str/{id}/submit":                    pathSubmitSTRReport(),
			"/api/v1/reports/str/export":                         pathExportSTRReport(),
			"/api/v1/cases":                                      pathCases(),
			"/api/v1/cases/{id}":                                 pathCase(),
			"/api/v1/cases/{id}/notes":                           pathCaseNote(),
			"/api/v1/cases/{id}/timeline":                        pathCaseTimeline(),
			"/api/v1/cases/{id}/export":                          pathExportCaseFile(),
			"/api/v1/cases/{id}/evidence":                        pathAddCaseEvidence(),
			"/api/v1/cases/{id}/evidence/{evidence}/corrections": pathCorrectCaseEvidence(),
			"/api/v1/cases/{id}/checklist/{item}":                pathUpdateCaseChecklist(),
			"/api/v1/cases/{id}/work-items":                      pathCreateCaseWorkItem(),
			"/api/v1/cases/{id}/work-items/{item}":               pathUpdateCaseWorkItem(),
			"/api/v1/cases/{id}/related":                         pathRelatedCases(),
			"/api/v1/cases/{id}/related/{relationship}":          pathRelatedCaseMutation(),
			"/api/v1/dashboard":                                  pathGET("Dashboard statistics"),
			"/api/v1/batch/score":                                pathPOST("Batch score customers"),
			"/api/v1/batch/monitor":                              pathPOST("Batch monitor transactions"),
			"/api/v1/webhooks/inbound/customer-status":           pathCustomerStatusWebhook(),
			"/api/v1/webhooks":                                   pathWebhooks(),
			"/api/v1/webhooks/{id}":                              pathWebhook(),
			"/api/v1/webhooks/{id}/deliveries":                   pathWebhookDeliveries(),
			"/api/v1/webhooks/dlq":                               pathWebhookDLQ(),
			"/api/v1/webhooks/dlq/{id}/reprocess":                pathWebhookReprocess(),
			"/api/v1/admin/apikeys":                              pathCRUD("API Key", "apikeys"),
			"/api/v1/admin/apikeys/{id}":                         pathDELETE("Revoke API key"),
			"/api/v1/operators":                                  pathOperatorDirectory(),
			"/api/v1/audit":                                      pathAuditLogs(),
			"/api/v1/audit/export":                               pathExportAuditLogs(),
			"/api/v1/system/config-digests":                      pathGET("Get loaded configuration digests"),
			"/api/v1/accounts":                                   pathAccountCreate(),
			"/api/v1/accounts/{id}":                              pathAccountGet(),
			"/api/v1/accounts/{id}/customers":                    pathAccountCustomers(),
			"/api/v1/admin/retention-policies":                   pathRetentionPolicies(),
			"/api/v1/admin/retention-policies/{category}":        pathRetentionPolicy(),
			"/api/v1/admin/users":                                pathUsers(),
			"/api/v1/auth/login":                                 pathLogin(),
			"/api/v1/auth/logout":                                pathLogout(),
			"/api/v1/auth/refresh":                               pathRefresh(),
			"/api/v1/auth/me":                                    pathMe(),
			"/api/v1/config/validate":                            pathValidateConfig(),
			"/api/v1/openapi.json":                               pathOpenAPIDocument(),
			"/api/v1/rules":                                      pathRules(),
			"/api/v1/rules/{id}":                                 pathRule(),
			"/api/v1/rules/{id}/export":                          pathRuleExport(),
			"/api/v1/rules/{id}/activate":                        pathRuleActivation("activate"),
			"/api/v1/rules/{id}/deactivate":                      pathRuleActivation("deactivate"),
			"/api/v1/rules/import":                               pathRuleImport(),
			"/api/v1/screening/check":                            pathScreeningCheck(),
			"/api/v1/screening/results/{id}":                     pathScreeningResult(),
			"/api/v1/setup":                                      pathSetup(),
			"/api/v1/system/info":                                pathSystemInfo(),
			"/api/v1/whitelist":                                  pathWhitelist(),
			"/api/v1/whitelist/{id}":                             pathWhitelistEntry(),
			"/api/v1/whitelist/{id}/approve":                     pathWhitelistApprove(),
			"/api/v1/whitelist/{id}/reviews":                     pathWhitelistReview(),
			"/api/v1/whitelist/{id}/revoke":                      pathWhitelistRevoke(),
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":        "http",
					"scheme":      "bearer",
					"description": "API key authentication",
				},
			},
			"schemas": schemas,
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

func paginatedAlertListResponses() map[string]any {
	return successWithErrors("200", "Paginated alerts", objectSchema(map[string]any{
		"data":       arraySchema(schemaRef("Alert")),
		"pagination": schemaRef("PaginationMeta"),
	}, "data", "pagination"), "400", "401", "404", "429", "500", "503")
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
	params = append(params, queueFilterParams()...)
	return append(params, map[string]any{
		"name":        "sort",
		"in":          "query",
		"description": "Optional queue sort; risk ranks critical > high > medium > low, then updated_at DESC and canonical ID DESC",
		"schema":      map[string]any{"type": "string", "enum": []string{"risk"}},
	})
}

func queueFilterParams() []map[string]any {
	stringParam := func(name, description string) map[string]any {
		return map[string]any{
			"name":        name,
			"in":          "query",
			"description": description,
			"schema":      map[string]any{"type": "string"},
		}
	}
	boolParam := func(name, description string) map[string]any {
		return map[string]any{
			"name":        name,
			"in":          "query",
			"description": description,
			"schema":      map[string]any{"type": "boolean"},
		}
	}
	return []map[string]any{
		stringParam("customer_id", "Filter by customer ID"),
		stringParam("status", "Comma-separated status values"),
		boolParam("active", "Restrict to active queue items"),
		boolParam("terminal", "Restrict to terminal queue items"),
		stringParam("assignee", "Filter by assigned operator ID"),
		boolParam("mine", "Filter by the authenticated operator"),
		stringParam("team", "Filter by assigned team"),
		boolParam("unassigned", "Restrict to unassigned items"),
		stringParam("severity", "Alert severity"),
		stringParam("scenario_id", "Alert scenario ID"),
		stringParam("priority", "Case priority"),
		stringParam("disposition", "Case disposition"),
		boolParam("str_candidate", "Case STR candidacy"),
		stringParam("search", "Search customer, alert, or case text"),
		boolParam("overdue", "Restrict to overdue items"),
		map[string]any{
			"name":        "min_age_days",
			"in":          "query",
			"description": "Minimum item age in days (1-36500)",
			"schema":      map[string]any{"type": "integer", "minimum": 1, "maximum": 36500},
		},
		map[string]any{
			"name":        "max_age_days",
			"in":          "query",
			"description": "Maximum item age in days (1-36500)",
			"schema":      map[string]any{"type": "integer", "minimum": 1, "maximum": 36500},
		},
	}
}

func schemaRef(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func arraySchema(item any) map[string]any {
	return map[string]any{"type": "array", "items": item}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func jsonRequestBody(schema any) map[string]any {
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{"schema": schema},
		},
	}
}

func jsonResponse(description string, schema any) map[string]any {
	response := map[string]any{"description": description}
	if schema != nil {
		response["content"] = map[string]any{
			"application/json": map[string]any{"schema": schema},
		}
	}
	return response
}

func openapiErrorResponse(description string) map[string]any {
	return jsonResponse(description, schemaRef("Error"))
}

func standardErrors(statuses ...string) map[string]any {
	responses := make(map[string]any, len(statuses))
	for _, status := range statuses {
		responses[status] = openapiErrorResponse(statusDescription(status))
	}
	return responses
}

func statusDescription(status string) string {
	switch status {
	case "400":
		return "Validation failed"
	case "401":
		return "Authentication required"
	case "403":
		return "Permission denied"
	case "404":
		return "Resource not found"
	case "409":
		return "Optimistic-lock or lifecycle conflict"
	case "422":
		return "Business validation failed"
	case "429":
		return "Rate limit exceeded"
	case "500":
		return "Internal repository failure"
	case "503":
		return "Required storage is unavailable"
	default:
		return "HTTP " + status
	}
}

func successWithErrors(status string, description string, schema any, errors ...string) map[string]any {
	responses := standardErrors(errors...)
	responses[status] = jsonResponse(description, schema)
	return responses
}

func pathIDParameter(name, description string) map[string]any {
	return map[string]any{
		"name": name, "in": "path", "required": true, "description": description,
		"schema": map[string]any{"type": "string"},
	}
}

func wave2Schemas() map[string]any {
	stringProperties := func(names ...string) map[string]any {
		properties := make(map[string]any, len(names))
		for _, name := range names {
			properties[name] = map[string]any{"type": "string"}
		}
		return properties
	}

	schemas := map[string]any{
		"Error": objectSchema(map[string]any{
			"error":      map[string]any{"type": "string"},
			"error_code": map[string]any{"type": "string"},
		}, "error", "error_code"),
		"Alert": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"},
			"scenario_id": map[string]any{"type": "string"}, "severity": map[string]any{"type": "string"},
			"status": map[string]any{"type": "string"}, "score": map[string]any{"type": "number"},
			"description": map[string]any{"type": "string"}, "transaction_ids": arraySchema(map[string]any{"type": "string"}),
			"updated_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"Customer": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "external_id": map[string]any{"type": "string"},
			"customer_type": map[string]any{"type": "string"}, "country_code": map[string]any{"type": "string"},
			"product_types": arraySchema(map[string]any{"type": "string"}), "attributes": map[string]any{"type": "object", "additionalProperties": true},
			"status":     map[string]any{"type": "string", "enum": []string{"active", "dormant", "frozen", "closed"}},
			"risk_score": map[string]any{"type": "number", "nullable": true}, "risk_tier": map[string]any{"type": "string", "nullable": true},
			"last_scored_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"created_at":     map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "external_id", "customer_type", "country_code", "product_types", "attributes", "status", "created_at", "updated_at"),
		"CustomerStatusWebhookRequest": objectSchema(map[string]any{
			"external_id": map[string]any{"type": "string"},
			"status":      map[string]any{"type": "string", "enum": []string{"active", "dormant", "frozen", "closed"}},
			"reason":      map[string]any{"type": "string"},
		}, "external_id", "status"),
		"AlertUpdateRequest": objectSchema(map[string]any{
			"status":              map[string]any{"type": "string"},
			"resolved_by":         map[string]any{"type": "string"},
			"rationale":           map[string]any{"type": "string"},
			"confirm":             map[string]any{"type": "boolean"},
			"assigned_to":         map[string]any{"type": "string"},
			"assigned_team":       map[string]any{"type": "string"},
			"due_at":              map[string]any{"type": "string", "format": "date-time"},
			"clear_due_at":        map[string]any{"type": "boolean"},
			"expected_updated_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"BulkCloseAlertsRequest": objectSchema(map[string]any{
			"scenario_id": map[string]any{"type": "string"},
			"period_from": map[string]any{"type": "string", "format": "date-time"},
			"period_to":   map[string]any{"type": "string", "format": "date-time"},
			"severity":    map[string]any{"type": "string", "enum": []string{"low", "medium", "high", "critical"}},
			"reason":      map[string]any{"type": "string"},
		}, "reason"),
		"BulkCloseAlertsResponse": objectSchema(map[string]any{
			"closed_count": map[string]any{"type": "integer", "minimum": 0},
			"alert_ids":    arraySchema(map[string]any{"type": "string"}),
		}, "closed_count", "alert_ids"),
		"BulkCaseAssignmentRequest": objectSchema(map[string]any{
			"alert_ids":   arraySchema(map[string]any{"type": "string"}),
			"case_id":     map[string]any{"type": "string"},
			"customer_id": map[string]any{"type": "string"},
			"summary":     map[string]any{"type": "string"},
		}, "alert_ids"),
		"BulkCaseAssignmentResponse": objectSchema(map[string]any{
			"case_id": map[string]any{"type": "string"},
			"created": map[string]any{"type": "boolean"},
		}, "case_id", "created"),
		"Case": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"},
			"alert_ids": arraySchema(map[string]any{"type": "string"}), "status": map[string]any{"type": "string"},
			"priority": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"},
			"str_candidate": map[string]any{"type": "boolean"}, "str_report_id": map[string]any{"type": "string", "nullable": true},
			"str_filed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"updated_at":   map[string]any{"type": "string", "format": "date-time"},
		}),
		"CaseNote": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "author": map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "author", "content", "created_at"),
		"CaseEvent": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "case_id": map[string]any{"type": "string"},
			"event_type": map[string]any{"type": "string"}, "actor": map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"}, "before": map[string]any{"type": "object", "additionalProperties": true},
			"after":          map[string]any{"type": "object", "additionalProperties": true},
			"correlation_id": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"CaseEvidence": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "case_id": map[string]any{"type": "string"},
			"root_id": map[string]any{"type": "string"}, "supersedes_id": map[string]any{"type": "string", "nullable": true},
			"description": map[string]any{"type": "string"}, "source": map[string]any{"type": "string"},
			"evidence_type": map[string]any{"type": "string"}, "collected_by": map[string]any{"type": "string"},
			"version": map[string]any{"type": "integer", "minimum": 1}, "created_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "case_id", "root_id", "description", "source", "evidence_type", "collected_by", "version", "created_at"),
		"CaseChecklistItem": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "case_id": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"},
			"label": map[string]any{"type": "string"}, "completed": map[string]any{"type": "boolean"}, "version": map[string]any{"type": "integer"},
		}, "id", "case_id", "key", "label", "completed", "version"),
		"CaseWorkItem": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "case_id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}, "assigned_to": map[string]any{"type": "string"},
			"due_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
		}, "id", "case_id", "title", "status"),
		"CaseRelationship": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "case_id": map[string]any{"type": "string"}, "related_case_id": map[string]any{"type": "string"},
			"relationship_type": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"},
			"active": map[string]any{"type": "boolean"}, "created_by": map[string]any{"type": "string"},
			"created_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "case_id", "related_case_id", "relationship_type", "rationale", "active"),
		"RelatedCase": objectSchema(map[string]any{
			"case": schemaRef("Case"), "link_type": map[string]any{"type": "string", "enum": []string{"auto", "manual"}},
			"relationship": schemaRef("CaseRelationship"),
		}, "case", "link_type"),
		"AlertDecision": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "alert_id": map[string]any{"type": "string"},
			"from_status": map[string]any{"type": "string"}, "to_status": map[string]any{"type": "string"},
			"outcome": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"},
			"actor": map[string]any{"type": "string"}, "supersedes_id": map[string]any{"type": "string", "nullable": true},
			"created_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "alert_id", "from_status", "to_status", "rationale", "actor", "created_at"),
		"STRReport": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "alert_id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"},
			"case_id": map[string]any{"type": "string"}, "corrects_report_id": map[string]any{"type": "string", "nullable": true},
			"supersedes_report_id": map[string]any{"type": "string", "nullable": true}, "report_type": map[string]any{"type": "string", "enum": []string{"str"}},
			"status": map[string]any{"type": "string", "enum": []string{"draft", "submitted"}}, "suspicious_point": map[string]any{"type": "string"},
			"transaction_ids": arraySchema(map[string]any{"type": "string"}), "total_amount": map[string]any{"type": "number"},
			"currency": map[string]any{"type": "string"}, "alert_snapshot": map[string]any{"type": "object", "additionalProperties": true},
			"customer_snapshot": map[string]any{"type": "object", "additionalProperties": true},
			"created_at":        map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
			"submitted_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "submitted_by": map[string]any{"type": "string"},
			"submission_evidence": map[string]any{"type": "string"},
		}, "id", "alert_id", "customer_id", "report_type", "status", "suspicious_point", "transaction_ids", "total_amount", "currency", "created_at", "updated_at"),
		"STRReportExport": objectSchema(map[string]any{
			"report_id": map[string]any{"type": "string"}, "report_status": map[string]any{"type": "string"}, "report_type": map[string]any{"type": "string"},
			"alert_id": map[string]any{"type": "string"}, "case_id": map[string]any{"type": "string"}, "corrects_report_id": map[string]any{"type": "string", "nullable": true},
			"supersedes_report_id": map[string]any{"type": "string", "nullable": true}, "customer": map[string]any{"type": "object", "additionalProperties": true},
			"alert": map[string]any{"type": "object", "additionalProperties": true}, "transactions": arraySchema(map[string]any{"type": "object", "additionalProperties": true}),
			"export_version": map[string]any{"type": "string"}, "exported_at": map[string]any{"type": "string", "format": "date-time"},
		}, "report_id", "report_status", "report_type", "alert_id", "case_id", "export_version", "exported_at"),
		"CaseFile": objectSchema(map[string]any{
			"export_version": map[string]any{"type": "string"}, "exported_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"case": schemaRef("Case"), "events": arraySchema(schemaRef("CaseEvent")), "evidence": arraySchema(schemaRef("CaseEvidence")),
			"checklist": arraySchema(schemaRef("CaseChecklistItem")), "work_items": arraySchema(schemaRef("CaseWorkItem")),
			"relationships": arraySchema(schemaRef("CaseRelationship")), "event_pagination": schemaRef("PaginationMeta"),
		}, "case", "events", "evidence", "checklist", "work_items", "relationships"),
		"AuditEntry": objectSchema(map[string]any{
			"id": map[string]any{"type": "integer"}, "user_id": map[string]any{"type": "string"}, "action": map[string]any{"type": "string"},
			"resource_type": map[string]any{"type": "string"}, "resource_id": map[string]any{"type": "string"}, "details": map[string]any{"type": "object", "additionalProperties": true},
			"created_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "user_id", "action", "resource_type", "resource_id", "created_at"),
		"OperatorDirectory": objectSchema(map[string]any{
			"users": arraySchema(objectSchema(stringProperties("id", "email", "role"))), "teams": arraySchema(map[string]any{"type": "string"}),
		}, "users", "teams"),
		"PaginatedSTRReports":   objectSchema(map[string]any{"data": arraySchema(schemaRef("STRReport")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"PaginatedCases":        objectSchema(map[string]any{"data": arraySchema(schemaRef("Case")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"PaginatedAuditEntries": objectSchema(map[string]any{"data": arraySchema(schemaRef("AuditEntry")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"STRCreateRequest": objectSchema(map[string]any{
			"alert_id": map[string]any{"type": "string"}, "case_id": map[string]any{"type": "string"}, "suspicious_point": map[string]any{"type": "string"},
			"created_by": map[string]any{"type": "string"}, "corrects_report_id": map[string]any{"type": "string"}, "supersedes_report_id": map[string]any{"type": "string"},
		}, "alert_id", "case_id", "suspicious_point"),
		"STRUpdateRequest": objectSchema(map[string]any{
			"suspicious_point": map[string]any{"type": "string"}, "expected_updated_at": map[string]any{"type": "string", "format": "date-time"},
		}, "suspicious_point"),
		"STRSubmitRequest": objectSchema(map[string]any{
			"submitted_by": map[string]any{"type": "string"}, "submission_evidence": map[string]any{"type": "string"}, "filing_reference": map[string]any{"type": "string"},
		}, "submission_evidence"),
		"CaseCreateRequest": objectSchema(map[string]any{
			"customer_id": map[string]any{"type": "string"}, "alert_ids": arraySchema(map[string]any{"type": "string"}), "priority": map[string]any{"type": "string"},
			"priority_rationale": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"}, "assigned_to": map[string]any{"type": "string"},
			"assigned_team": map[string]any{"type": "string"}, "due_at": map[string]any{"type": "string", "format": "date-time"}, "str_candidate": map[string]any{"type": "boolean"},
		}, "customer_id", "summary"),
		"CaseUpdateRequest": objectSchema(map[string]any{
			"status": map[string]any{"type": "string"}, "priority": map[string]any{"type": "string"}, "priority_rationale": map[string]any{"type": "string"},
			"assigned_to": map[string]any{"type": "string"}, "assigned_team": map[string]any{"type": "string"}, "due_at": map[string]any{"type": "string", "format": "date-time"},
			"clear_due_at": map[string]any{"type": "boolean"}, "summary": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"},
			"rationale": map[string]any{"type": "string"}, "confirm": map[string]any{"type": "boolean"}, "str_report_id": map[string]any{"type": "string"},
			"filing_channel": map[string]any{"type": "string"}, "destination": map[string]any{"type": "string"}, "external_reference": map[string]any{"type": "string"},
			"investigation_disposition": map[string]any{"type": "string"}, "str_candidate": map[string]any{"type": "boolean"},
			"expected_updated_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"CaseNoteRequest": objectSchema(map[string]any{"author": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "content"),
		"CaseEvidenceCreateRequest": objectSchema(map[string]any{
			"description": map[string]any{"type": "string"}, "source": map[string]any{"type": "string"}, "evidence_type": map[string]any{"type": "string"},
			"collected_at": map[string]any{"type": "string", "format": "date-time"}, "collected_by": map[string]any{"type": "string"}, "integrity_hash": map[string]any{"type": "string"},
		}, "description", "source", "evidence_type", "collected_by"),
		"CaseEvidenceCorrectionRequest": objectSchema(map[string]any{
			"description": map[string]any{"type": "string"}, "source": map[string]any{"type": "string"}, "evidence_type": map[string]any{"type": "string"},
			"collected_at": map[string]any{"type": "string", "format": "date-time"}, "collected_by": map[string]any{"type": "string"}, "integrity_hash": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"},
		}, "reason"),
		"CaseChecklistRequest": objectSchema(map[string]any{"label": map[string]any{"type": "string"}, "completed": map[string]any{"type": "boolean"}}, "label", "completed"),
		"CaseWorkItemRequest": objectSchema(map[string]any{
			"title": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"open", "in_progress", "completed", "cancelled"}},
			"assigned_to": map[string]any{"type": "string"}, "due_at": map[string]any{"type": "string", "format": "date-time"},
		}, "title"),
		"RelatedCaseCreateRequest": objectSchema(map[string]any{
			"related_case_id": map[string]any{"type": "string"}, "relationship_type": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"},
			"expected_updated_at": map[string]any{"type": "string", "format": "date-time"},
		}, "related_case_id", "rationale"),
		"RelatedCaseCorrectionRequest": objectSchema(map[string]any{"relationship_type": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}}, "rationale"),
		"RelatedCaseRemoveRequest":     objectSchema(map[string]any{"reason": map[string]any{"type": "string"}, "expected_updated_at": map[string]any{"type": "string", "format": "date-time"}}, "reason"),
	}
	return schemas
}

// compatibilitySchemas covers the older but still callable API surface. They
// remain explicit components so the OpenAPI document describes the same
// request fields and nullable response shapes as the handlers, without
// changing the public JSON contract of the Wave 0/1 endpoints.
func compatibilitySchemas() map[string]any {
	return map[string]any{
		"Account": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "external_id": map[string]any{"type": "string"},
			"account_type": map[string]any{"type": "string", "enum": []string{"individual", "joint"}},
			"created_at":   map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "external_id", "account_type", "created_at", "updated_at"),
		"AccountCustomer": objectSchema(map[string]any{
			"account_id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"},
			"role": map[string]any{"type": "string", "enum": []string{"primary", "co_holder"}},
		}, "account_id", "customer_id", "role"),
		"RuleDefinition": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"}, "definition": map[string]any{"type": "object", "additionalProperties": true},
			"version": map[string]any{"type": "integer"}, "is_active": map[string]any{"type": "boolean"}, "created_by": map[string]any{"type": "string"},
			"created_at": map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "type", "name", "definition", "version", "is_active", "created_at", "updated_at"),
		"RuleImportItem": objectSchema(map[string]any{
			"type": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "definition": map[string]any{"type": "object", "additionalProperties": true},
		}, "type", "name", "definition"),
		"ScoreRecord": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "rule_set_id": map[string]any{"type": "string"},
			"score": map[string]any{"type": "number"}, "tier": map[string]any{"type": "string"}, "scored_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "customer_id", "score", "tier", "scored_at"),
		"PaginatedRules": objectSchema(map[string]any{"data": arraySchema(schemaRef("RuleDefinition")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"ScreeningResult": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "list_id": map[string]any{"type": "string"},
			"list_type": map[string]any{"type": "string"}, "entry_id": map[string]any{"type": "string"}, "matched_name": map[string]any{"type": "string"},
			"similarity": map[string]any{"type": "number"}, "status": map[string]any{"type": "string"}, "false_positive_reason": map[string]any{"type": "string"},
			"reviewed_by": map[string]any{"type": "string"}, "reviewed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"screened_at": map[string]any{"type": "string", "format": "date-time"}, "created_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"RetentionPolicy": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "data_category": map[string]any{"type": "string"}, "retention_days": map[string]any{"type": "integer", "minimum": 1},
			"min_retention_days": map[string]any{"type": "integer", "nullable": true}, "updated_by": map[string]any{"type": "string"},
			"created_at": map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"WhitelistEntry": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"}, "excluded_rule_ids": arraySchema(map[string]any{"type": "string"}),
			"valid_from": map[string]any{"type": "string", "format": "date-time"}, "valid_until": map[string]any{"type": "string", "format": "date-time"},
			"requested_by": map[string]any{"type": "string"}, "approved_by": map[string]any{"type": "string", "nullable": true},
			"approved_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "revoked_by": map[string]any{"type": "string", "nullable": true},
			"revoked_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "version": map[string]any{"type": "integer"},
			"created_at": map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"WhitelistReview": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "whitelist_entry_id": map[string]any{"type": "string"}, "reviewed_by": map[string]any{"type": "string"},
			"decision": map[string]any{"type": "string", "enum": []string{"renewed", "revoked"}}, "review_notes": map[string]any{"type": "string"},
			"next_review_date": map[string]any{"type": "string", "format": "date", "nullable": true}, "created_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"UserProfile":            objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "email": map[string]any{"type": "string", "format": "email"}, "role": map[string]any{"type": "string"}}, "id", "email", "role"),
		"ConfigValidationResult": objectSchema(map[string]any{"valid": map[string]any{"type": "boolean"}, "errors": arraySchema(map[string]any{"type": "object", "additionalProperties": true})}, "valid", "errors"),
		"SystemInfo":             objectSchema(map[string]any{"version": map[string]any{"type": "string"}, "components": arraySchema(map[string]any{"type": "string"}), "endpoints": map[string]any{"type": "integer"}, "features": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "boolean"}}}),
		"Webhook": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "url": map[string]any{"type": "string", "format": "uri"},
			"events": arraySchema(map[string]any{"type": "string"}), "active": map[string]any{"type": "boolean"},
			"created_at": map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "url", "events", "active", "created_at", "updated_at"),
		"WebhookDelivery": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "webhook_id": map[string]any{"type": "string"}, "event": map[string]any{"type": "string"},
			"payload": map[string]any{"type": "string"}, "status_code": map[string]any{"type": "integer"}, "success": map[string]any{"type": "boolean"},
			"error": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"},
			"event_id": map[string]any{"type": "string"}, "attempt_count": map[string]any{"type": "integer"},
			"next_attempt_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
		}, "id", "webhook_id", "event", "payload", "status_code", "success", "created_at", "event_id", "attempt_count"),
		"DLQEntry": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "webhook_id": map[string]any{"type": "string"}, "event_id": map[string]any{"type": "string"},
			"event": map[string]any{"type": "string"}, "payload": map[string]any{"type": "string"}, "attempt_count": map[string]any{"type": "integer"},
			"last_error": map[string]any{"type": "string"}, "failed_at": map[string]any{"type": "string", "format": "date-time"},
			"reprocessed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
		}, "id", "webhook_id", "event_id", "event", "payload", "attempt_count", "failed_at"),
		"WebhookCreateRequest": objectSchema(map[string]any{
			"url": map[string]any{"type": "string", "format": "uri"}, "events": arraySchema(map[string]any{"type": "string"}), "secret": map[string]any{"type": "string", "writeOnly": true},
		}, "url", "events"),
		"WebhookReprocessResponse": objectSchema(map[string]any{"success": map[string]any{"type": "boolean"}, "status_code": map[string]any{"type": "integer"}}, "success", "status_code"),
	}
}

func documentedJSONOperation(summary string, parameters []map[string]any, request any, status, description string, response any, errorStatuses ...string) map[string]any {
	op := map[string]any{
		"summary":   summary,
		"responses": successWithErrors(status, description, response, errorStatuses...),
	}
	if len(parameters) > 0 {
		op["parameters"] = parameters
	}
	if request != nil {
		op["requestBody"] = jsonRequestBody(request)
	}
	return op
}

func publicOperation(operation map[string]any) map[string]any {
	operation["security"] = []map[string]any{}
	return operation
}

func pathAccountCreate() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Create a joint or individual account", nil,
		objectSchema(map[string]any{"external_id": map[string]any{"type": "string"}, "account_type": map[string]any{"type": "string", "enum": []string{"individual", "joint"}}}, "external_id", "account_type"),
		"201", "Created account", schemaRef("Account"), "400", "401", "403", "500")}
}

func pathAccountGet() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get an account", []map[string]any{pathIDParameter("id", "Account identifier")}, nil,
		"200", "Account", schemaRef("Account"), "401", "404", "500")}
}

func pathAccountCustomers() map[string]any {
	parameters := []map[string]any{pathIDParameter("id", "Account identifier")}
	return map[string]any{
		"get": documentedJSONOperation("List customers linked to an account", parameters, nil, "200", "Account customers", arraySchema(schemaRef("AccountCustomer")), "401", "404", "500"),
		"post": documentedJSONOperation("Add a customer to an account", parameters,
			objectSchema(map[string]any{"customer_id": map[string]any{"type": "string"}, "role": map[string]any{"type": "string", "enum": []string{"primary", "co_holder"}}}, "customer_id", "role"),
			"201", "Account customer link created", objectSchema(map[string]any{"status": map[string]any{"type": "string"}}, "status"), "400", "401", "403", "404", "500"),
	}
}

func pathRetentionPolicies() map[string]any {
	return map[string]any{"get": documentedJSONOperation("List retention policies", nil, nil, "200", "Retention policies", arraySchema(schemaRef("RetentionPolicy")), "401", "403", "500", "503")}
}

func pathRetentionPolicy() map[string]any {
	return map[string]any{"put": documentedJSONOperation("Update a retention policy", []map[string]any{pathIDParameter("category", "Data category")},
		objectSchema(map[string]any{"retention_days": map[string]any{"type": "integer", "minimum": 1}}, "retention_days"),
		"200", "Updated retention policy", schemaRef("RetentionPolicy"), "400", "401", "403", "404", "500", "503")}
}

func pathUsers() map[string]any {
	return map[string]any{"get": documentedJSONOperation("List users", nil, nil, "200", "Users", arraySchema(schemaRef("UserProfile")), "401", "403", "500", "503")}
}

func pathLogin() map[string]any {
	return map[string]any{"post": publicOperation(documentedJSONOperation("Create a session", nil,
		objectSchema(map[string]any{"email": map[string]any{"type": "string", "format": "email"}, "password": map[string]any{"type": "string", "format": "password"}}, "email", "password"),
		"200", "Authenticated user profile", schemaRef("UserProfile"), "400", "401", "500", "503"))}
}

func pathLogout() map[string]any {
	return map[string]any{"post": publicOperation(documentedJSONOperation("End the current session", nil, nil, "200", "Logged out", objectSchema(map[string]any{"status": map[string]any{"type": "string"}}, "status")))}
}

func pathRefresh() map[string]any {
	return map[string]any{"post": publicOperation(documentedJSONOperation("Rotate the refresh token", nil, nil, "200", "Session refreshed", objectSchema(map[string]any{"status": map[string]any{"type": "string"}}, "status"), "401", "500", "503"))}
}

func pathMe() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get the authenticated user profile", nil, nil, "200", "Authenticated user profile", schemaRef("UserProfile"), "401", "500", "503")}
}

func pathValidateConfig() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Validate a configuration document", nil,
		objectSchema(map[string]any{"config_type": map[string]any{"type": "string"}, "yaml_content": map[string]any{"type": "string", "maxLength": 524288}}, "config_type", "yaml_content"),
		"200", "Validation result", schemaRef("ConfigValidationResult"), "400", "401", "403", "500", "503")}
}

func pathOpenAPIDocument() map[string]any {
	return map[string]any{"get": jsonOperationWithoutError("Get the live OpenAPI document", nil, "200", "OpenAPI document", map[string]any{"type": "object", "additionalProperties": true})}
}

func jsonOperationWithoutError(summary string, parameters []map[string]any, status, description string, response any) map[string]any {
	op := map[string]any{"summary": summary, "responses": map[string]any{status: jsonResponse(description, response)}}
	if len(parameters) > 0 {
		op["parameters"] = parameters
	}
	return op
}

func pathScoreCustomer() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Score a customer's CDD risk", []map[string]any{pathIDParameter("id", "Customer identifier")},
		objectSchema(map[string]any{"rule_set_id": map[string]any{"type": "string"}}),
		"200", "Score record", schemaRef("ScoreRecord"), "400", "401", "404", "500", "502", "503")}
}

func pathScreenCustomer() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Screen a customer against configured lists", []map[string]any{pathIDParameter("id", "Customer identifier")},
		objectSchema(map[string]any{"list_ids": arraySchema(map[string]any{"type": "string"})}),
		"200", "Screening result", map[string]any{"type": "object", "additionalProperties": true}, "400", "401", "404", "500", "502", "503")}
}

func pathScoreHistory() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get customer score history", []map[string]any{pathIDParameter("id", "Customer identifier")}, nil,
		"200", "Score history", arraySchema(schemaRef("ScoreRecord")), "401", "404", "500", "503")}
}

func pathRules() map[string]any {
	parameters := append([]map[string]any{}, paginationParams()...)
	parameters = append(parameters,
		map[string]any{"name": "type", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "is_active", "in": "query", "schema": map[string]any{"type": "boolean", "default": false}},
	)
	return map[string]any{
		"get": documentedJSONOperation("List versioned rule definitions", parameters, nil, "200", "Paginated rules", schemaRef("PaginatedRules"), "400", "401", "429", "500", "503"),
		"post": documentedJSONOperation("Create an inactive rule definition", nil,
			objectSchema(map[string]any{"type": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "definition": map[string]any{"type": "object", "additionalProperties": true}, "is_active": map[string]any{"type": "boolean"}}, "type", "name", "definition"),
			"201", "Created rule", schemaRef("RuleDefinition"), "400", "401", "403", "500", "503"),
	}
}

func pathRule() map[string]any {
	parameters := []map[string]any{pathIDParameter("id", "Rule name or identifier")}
	getParameters := append([]map[string]any{}, parameters...)
	getParameters = append(getParameters, map[string]any{"name": "version", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1}})
	return map[string]any{
		"get": documentedJSONOperation("Get a rule definition or a selected version", getParameters, nil, "200", "Rule definition", schemaRef("RuleDefinition"), "400", "401", "404", "500", "503"),
		"put": documentedJSONOperation("Append a new rule version", parameters,
			objectSchema(map[string]any{"description": map[string]any{"type": "string"}, "definition": map[string]any{"type": "object", "additionalProperties": true}, "is_active": map[string]any{"type": "boolean"}}, "definition"),
			"200", "Updated rule version", schemaRef("RuleDefinition"), "400", "401", "403", "404", "500", "503"),
	}
}

func pathRuleExport() map[string]any {
	responses := standardErrors("400", "401", "404", "500", "503")
	responses["200"] = map[string]any{
		"description": "Rule export",
		"content": map[string]any{
			"application/json":   map[string]any{"schema": schemaRef("RuleImportItem")},
			"application/x-yaml": map[string]any{"schema": map[string]any{"type": "string"}},
		},
	}
	return map[string]any{"get": map[string]any{
		"summary": "Export a rule as JSON or YAML", "parameters": []map[string]any{pathIDParameter("id", "Rule name or identifier"), {"name": "format", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"json", "yaml"}, "default": "json"}}}, "responses": responses,
	}}
}

func pathRuleActivation(action string) map[string]any {
	return map[string]any{"post": documentedJSONOperation(""+strings.Title(action)+" a rule version", []map[string]any{pathIDParameter("id", "Rule name or identifier")}, nil, "200", "Current rule definition", schemaRef("RuleDefinition"), "401", "403", "404", "500", "503")}
}

func pathRuleImport() map[string]any {
	body := map[string]any{"required": true, "content": map[string]any{
		"application/json": map[string]any{"schema": arraySchema(schemaRef("RuleImportItem"))},
		"application/yaml": map[string]any{"schema": arraySchema(schemaRef("RuleImportItem"))},
		"text/yaml":        map[string]any{"schema": arraySchema(schemaRef("RuleImportItem"))},
	}}
	return map[string]any{"post": map[string]any{"summary": "Import a JSON or YAML rule batch", "requestBody": body, "responses": successWithErrors("201", "Created rules", arraySchema(schemaRef("RuleDefinition")), "400", "401", "403", "409", "500", "503")}}
}

func pathScreeningCheck() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Run an immediate screening check", nil,
		objectSchema(map[string]any{"customer_id": map[string]any{"type": "string"}, "list_ids": arraySchema(map[string]any{"type": "string"})}, "customer_id"),
		"200", "Screening batch result", map[string]any{"type": "object", "additionalProperties": true}, "400", "401", "404", "500", "503")}
}

func pathScreeningResult() map[string]any {
	return map[string]any{"patch": documentedJSONOperation("Review a screening result", []map[string]any{pathIDParameter("id", "Screening result identifier")},
		objectSchema(map[string]any{"status": map[string]any{"type": "string"}, "false_positive_reason": map[string]any{"type": "string"}, "reviewed_by": map[string]any{"type": "string"}}, "status"),
		"200", "Updated screening result", schemaRef("ScreeningResult"), "400", "401", "404", "500", "503")}
}

func pathSetup() map[string]any {
	return map[string]any{"post": publicOperation(documentedJSONOperation("Create the first administrator", nil,
		objectSchema(map[string]any{"email": map[string]any{"type": "string", "format": "email"}, "password": map[string]any{"type": "string", "format": "password"}}, "email", "password"),
		"201", "Administrator profile", schemaRef("UserProfile"), "400", "409", "500", "503"))}
}

func pathSystemInfo() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get runtime system information", nil, nil, "200", "System information", schemaRef("SystemInfo"))}
}

func pathWhitelist() map[string]any {
	parameters := []map[string]any{
		{"name": "status", "in": "query", "schema": map[string]any{"type": "string"}},
		{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 50, "maximum": 100}},
		{"name": "offset", "in": "query", "deprecated": true, "description": "Legacy offset pagination; the endpoint currently accepts offset only", "schema": map[string]any{"type": "integer", "default": 0}},
	}
	request := objectSchema(map[string]any{
		"customer_id": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}, "valid_until": map[string]any{"type": "string", "format": "date-time"}, "excluded_rule_ids": arraySchema(map[string]any{"type": "string"}),
	}, "customer_id", "reason", "valid_until")
	return map[string]any{
		"get":  documentedJSONOperation("List whitelist entries", parameters, nil, "200", "Paginated whitelist entries", objectSchema(map[string]any{"data": arraySchema(schemaRef("WhitelistEntry")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"), "401", "403", "500", "503"),
		"post": documentedJSONOperation("Request a whitelist entry", nil, request, "201", "Created whitelist entry", schemaRef("WhitelistEntry"), "400", "401", "403", "404", "409", "500", "503"),
	}
}

func pathWhitelistEntry() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get a whitelist entry", []map[string]any{pathIDParameter("id", "Whitelist entry identifier")}, nil, "200", "Whitelist entry", schemaRef("WhitelistEntry"), "401", "403", "404", "500", "503")}
}

func pathWhitelistApprove() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Approve a pending whitelist entry", []map[string]any{pathIDParameter("id", "Whitelist entry identifier")}, nil, "200", "Approved whitelist entry", schemaRef("WhitelistEntry"), "401", "403", "404", "409", "500", "503")}
}

func pathWhitelistRevoke() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Revoke a whitelist entry", []map[string]any{pathIDParameter("id", "Whitelist entry identifier")}, nil, "200", "Revoked whitelist entry", schemaRef("WhitelistEntry"), "401", "403", "404", "409", "500", "503")}
}

func pathWhitelistReview() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Record a whitelist review", []map[string]any{pathIDParameter("id", "Whitelist entry identifier")},
		objectSchema(map[string]any{"decision": map[string]any{"type": "string", "enum": []string{"renewed", "revoked"}}, "review_notes": map[string]any{"type": "string"}, "next_review_date": map[string]any{"type": "string", "format": "date"}, "new_valid_until": map[string]any{"type": "string", "format": "date-time"}}, "decision"),
		"201", "Whitelist review", schemaRef("WhitelistReview"), "400", "401", "403", "404", "409", "500", "503")}
}

func openapiPaginatedResponse(schemaName string) map[string]any {
	return jsonResponse("Success", schemaRef("Paginated"+schemaName))
}

func reportListParameters() []map[string]any {
	params := append([]map[string]any{}, paginationParams()...)
	params = append(params,
		map[string]any{"name": "status", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"draft", "submitted"}}},
		map[string]any{"name": "customer_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "alert_id", "in": "query", "schema": map[string]any{"type": "string"}},
	)
	return params
}

func pathAlertDecisionHistory() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "List immutable alert decision history",
			"parameters": []map[string]any{pathIDParameter("id", "Alert identifier")},
			"responses":  successWithErrors("200", "Decision history", arraySchema(schemaRef("AlertDecision")), "401", "404", "500", "503"),
		},
	}
}

func pathSTRReports() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "List durable STR reports",
			"parameters": reportListParameters(),
			"responses":  successWithErrors("200", "Paginated STR reports", schemaRef("PaginatedSTRReports"), "400", "401", "429", "500", "503"),
		},
		"post": map[string]any{
			"summary":     "Create an STR draft from a selected active STR-candidate case",
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("STRCreateRequest", []string{"alert_id", "case_id", "suspicious_point"})),
			"responses":   successWithErrors("201", "Created STR draft", schemaRef("STRReport"), "400", "401", "404", "409", "422", "500", "503"),
		},
	}
}

func pathSTRReport() map[string]any {
	parameters := []map[string]any{pathIDParameter("id", "STR report identifier")}
	update := map[string]any{
		"summary":     "Update a draft STR report with compare-and-swap protection when supplied",
		"parameters":  parameters,
		"requestBody": jsonRequestBody(schemaWithRequiredProperties("STRUpdateRequest", []string{"suspicious_point"})),
		"responses":   successWithErrors("200", "Updated STR draft", schemaRef("STRReport"), "400", "401", "404", "409", "500", "503"),
	}
	get := map[string]any{
		"summary":    "Get a durable STR report and its pinned snapshots",
		"parameters": parameters,
		"responses":  successWithErrors("200", "STR report", schemaRef("STRReport"), "401", "404", "500", "503"),
	}
	return map[string]any{"get": get, "put": update, "patch": update}
}

func pathSubmitSTRReport() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":    "Submit an STR report idempotently using submission evidence",
			"parameters": []map[string]any{pathIDParameter("id", "STR report identifier")},
			"requestBody": jsonRequestBody(map[string]any{
				"allOf":       []any{schemaRef("STRSubmitRequest")},
				"required":    []string{"submission_evidence"},
				"description": "Provide submission_evidence, or the additive filing_reference alias; repeating the same evidence is idempotent and a different value is a conflict.",
			}),
			"responses": successWithErrors("200", "Submitted STR report", schemaRef("STRReport"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathExportSTRReport() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary": "Export a durable STR snapshot as CSV or JSON; report_id is the stable route and alert_id is a deprecated compatibility path",
			"parameters": []map[string]any{
				{"name": "report_id", "in": "query", "description": "Durable report to export", "schema": map[string]any{"type": "string"}},
				{"name": "alert_id", "in": "query", "deprecated": true, "description": "Legacy source-alert export; use report_id for a pinned snapshot", "schema": map[string]any{"type": "string"}},
				{"name": "format", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"csv", "json"}, "default": "csv"}},
			},
			"responses": exportResponses(schemaRef("STRReportExport"), "str_{report_id}"),
		},
	}
}

func pathCases() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "List cases in one documented risk-ranked queue order",
			"parameters": riskQueuePaginationParams(),
			"responses":  successWithErrors("200", "Paginated cases", objectSchema(map[string]any{"data": arraySchema(schemaRef("Case")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"), "400", "401", "429", "500", "503"),
		},
		"post": map[string]any{
			"summary":     "Create a case and its required creation event atomically",
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CaseCreateRequest", []string{"customer_id", "summary"})),
			"responses":   successWithErrors("201", "Created case", schemaRef("Case"), "400", "401", "403", "404", "409", "500", "503"),
		},
	}
}

func pathCase() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "Get a case with its current lifecycle projection",
			"parameters": []map[string]any{pathIDParameter("id", "Case identifier")},
			"responses":  successWithErrors("200", "Case", schemaRef("Case"), "401", "404", "500", "503"),
		},
		"patch": map[string]any{
			"summary":     "Mutate a case lifecycle, assignment, filing, or CDD-derived priority",
			"parameters":  []map[string]any{pathIDParameter("id", "Case identifier")},
			"requestBody": jsonRequestBody(schemaRef("CaseUpdateRequest")),
			"responses":   successWithErrors("200", "Updated case", schemaRef("Case"), "400", "401", "403", "404", "409", "500", "503"),
		},
	}
}

func pathCaseNote() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Append a case note and timeline event atomically",
			"parameters":  []map[string]any{pathIDParameter("id", "Case identifier")},
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CaseNoteRequest", []string{"content"})),
			"responses":   successWithErrors("201", "Created case note", schemaRef("CaseNote"), "400", "401", "404", "500", "503"),
		},
	}
}

func pathExportCaseFile() map[string]any {
	responses := successWithErrors("200", "Case investigation file snapshot", schemaRef("CaseFile"), "401", "404", "500", "503")
	responses["200"].(map[string]any)["headers"] = map[string]any{
		"Content-Disposition": map[string]any{"description": "Attachment filename containing the case identifier", "schema": map[string]any{"type": "string"}},
	}
	return map[string]any{
		"get": map[string]any{
			"summary":    "Export the reproducible case investigation file",
			"parameters": []map[string]any{pathIDParameter("id", "Case identifier")},
			"responses":  responses,
		},
	}
}

func pathAddCaseEvidence() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Append version one evidence and its timeline event atomically",
			"parameters":  []map[string]any{pathIDParameter("id", "Case identifier")},
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CaseEvidenceCreateRequest", []string{"description", "source", "evidence_type", "collected_by"})),
			"responses":   successWithErrors("201", "Created evidence version", schemaRef("CaseEvidence"), "400", "401", "404", "500", "503"),
		},
	}
}

func pathCorrectCaseEvidence() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary": "Append a compare-and-swap evidence correction with a unique lineage version",
			"parameters": []map[string]any{
				pathIDParameter("id", "Case identifier"),
				pathIDParameter("evidence", "Current evidence version identifier"),
			},
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CaseEvidenceCorrectionRequest", []string{"reason"})),
			"responses":   successWithErrors("201", "Created corrected evidence version", schemaRef("CaseEvidence"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathUpdateCaseChecklist() map[string]any {
	return map[string]any{
		"put": map[string]any{
			"summary":     "Update a checklist item and append its timeline event atomically",
			"parameters":  []map[string]any{pathIDParameter("id", "Case identifier"), pathIDParameter("item", "Checklist key")},
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CaseChecklistRequest", []string{"label", "completed"})),
			"responses":   successWithErrors("200", "Updated checklist item", schemaRef("CaseChecklistItem"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathCreateCaseWorkItem() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Create a case work item and append its timeline event atomically",
			"parameters":  []map[string]any{pathIDParameter("id", "Case identifier")},
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CaseWorkItemRequest", []string{"title"})),
			"responses":   successWithErrors("201", "Created work item", schemaRef("CaseWorkItem"), "400", "401", "404", "500", "503"),
		},
	}
}

func pathUpdateCaseWorkItem() map[string]any {
	return map[string]any{
		"patch": map[string]any{
			"summary":     "Update a case work item and append its timeline event atomically",
			"parameters":  []map[string]any{pathIDParameter("id", "Case identifier"), pathIDParameter("item", "Work item identifier")},
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CaseWorkItemRequest", []string{"title"})),
			"responses":   successWithErrors("200", "Updated work item", schemaRef("CaseWorkItem"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathRelatedCases() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "List same-customer and manually related cases",
			"parameters": []map[string]any{pathIDParameter("id", "Case identifier"), {"name": "include_inactive", "in": "query", "schema": map[string]any{"type": "boolean", "default": false}}},
			"responses":  successWithErrors("200", "Related cases", arraySchema(schemaRef("RelatedCase")), "401", "404", "500", "503"),
		},
		"post": map[string]any{
			"summary":     "Add a same-customer related case link with immutable history",
			"parameters":  []map[string]any{pathIDParameter("id", "Case identifier")},
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("RelatedCaseCreateRequest", []string{"related_case_id", "rationale"})),
			"responses":   successWithErrors("200", "Updated case projection", schemaRef("Case"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathRelatedCaseMutation() map[string]any {
	parameters := []map[string]any{pathIDParameter("id", "Case identifier"), pathIDParameter("relationship", "Relationship identifier")}
	return map[string]any{
		"put": map[string]any{
			"summary":     "Correct a related-case link by appending a replacement row",
			"parameters":  parameters,
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("RelatedCaseCorrectionRequest", []string{"rationale"})),
			"responses":   successWithErrors("200", "Replacement relationship", schemaRef("CaseRelationship"), "400", "401", "404", "409", "500", "503"),
		},
		"delete": map[string]any{
			"summary":     "Remove a related-case link with an immutable reason",
			"parameters":  parameters,
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("RelatedCaseRemoveRequest", []string{"reason"})),
			"responses":   successWithErrors("200", "Removed relationship projection", objectSchema(map[string]any{"relationship_id": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}}, "relationship_id", "active"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathOperatorDirectory() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":   "List the durable active operator/team directory used for assignment validation",
			"responses": successWithErrors("200", "Operator directory", schemaRef("OperatorDirectory"), "401", "500", "503"),
		},
	}
}

func auditParameters() []map[string]any {
	params := append([]map[string]any{}, paginationParams()...)
	params = append(params,
		map[string]any{"name": "resource_type", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "resource_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "user_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "action_category", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "since", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
		map[string]any{"name": "until", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
	)
	return params
}

func pathAuditLogs() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "List filtered immutable audit logs",
			"parameters": auditParameters(),
			"responses":  successWithErrors("200", "Paginated audit logs", schemaRef("PaginatedAuditEntries"), "400", "401", "403", "429", "500", "503"),
		},
	}
}

func pathExportAuditLogs() map[string]any {
	responses := exportResponses(arraySchema(schemaRef("AuditEntry")), "audit_logs")
	return map[string]any{
		"get": map[string]any{
			"summary": "Export filtered audit logs as CSV or JSON using the same filters as the list endpoint",
			"parameters": append(auditParameters(), map[string]any{
				"name": "format", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"csv", "json"}, "default": "csv"},
			}),
			"responses": responses,
		},
	}
}

func schemaWithRequiredProperties(name string, required []string) map[string]any {
	schema := schemaRef(name)
	// The request schemas are named components; this helper is only used to
	// make the operation's required-field contract visible at the operation
	// boundary as well as in components.
	return map[string]any{"allOf": []any{schema}, "required": required}
}

func exportResponses(jsonSchema any, filename string) map[string]any {
	responses := standardErrors("400", "401", "403", "404", "422", "500", "503")
	responses["200"] = map[string]any{
		"description": "Download generated from the durable snapshot/filter result",
		"headers": map[string]any{
			"Content-Disposition": map[string]any{"description": "Attachment filename", "schema": map[string]any{"type": "string"}},
		},
		"content": map[string]any{
			"text/csv":         map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
			"application/json": map[string]any{"schema": jsonSchema},
		},
	}
	_ = filename // filename is retained at the call site for documentation clarity.
	return responses
}

func pathListRiskPaginated(summary string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    summary,
			"parameters": riskQueuePaginationParams(),
			"responses":  paginatedAlertListResponses(),
		},
	}
}

func pathAlert() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":    "Get Alert",
			"parameters": []map[string]any{pathIDParameter("id", "Alert identifier")},
			"responses":  successWithErrors("200", "Alert", schemaRef("Alert"), "401", "404", "500", "503"),
		},
		"patch": map[string]any{
			"summary":     "Update alert status, assignment, or due date atomically",
			"parameters":  []map[string]any{pathIDParameter("id", "Alert identifier")},
			"requestBody": jsonRequestBody(schemaRef("AlertUpdateRequest")),
			"responses":   successWithErrors("200", "Updated alert", schemaRef("Alert"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathBulkCloseAlerts() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Close filtered alerts with a required rationale and atomic decision/audit rows",
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("BulkCloseAlertsRequest", []string{"reason"})),
			"responses":   successWithErrors("200", "Bulk close result", schemaRef("BulkCloseAlertsResponse"), "400", "401", "409", "500", "503"),
		},
	}
}

func pathBulkCaseAssignment() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Assign alerts to an existing or newly created case atomically",
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("BulkCaseAssignmentRequest", []string{"alert_ids"})),
			"responses":   successWithErrors("200", "Bulk case assignment result", schemaRef("BulkCaseAssignmentResponse"), "400", "401", "404", "409", "500", "503"),
		},
	}
}

func pathCustomerStatusWebhook() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Apply a core-system customer status notification with an atomic audit record",
			"requestBody": jsonRequestBody(schemaWithRequiredProperties("CustomerStatusWebhookRequest", []string{"external_id", "status"})),
			"responses":   successWithErrors("200", "Updated customer", schemaRef("Customer"), "400", "401", "404", "500", "503"),
		},
	}
}

func pathWebhooks() map[string]any {
	return map[string]any{
		"get": documentedJSONOperation("List configured webhooks", nil, nil, "200", "Webhooks", arraySchema(schemaRef("Webhook")), "401", "500", "503"),
		"post": documentedJSONOperation("Create a webhook subscription", nil,
			schemaWithRequiredProperties("WebhookCreateRequest", []string{"url", "events"}),
			"201", "Created webhook", schemaRef("Webhook"), "400", "401", "500", "503"),
	}
}

func pathWebhook() map[string]any {
	parameters := []map[string]any{pathIDParameter("id", "Webhook identifier")}
	return map[string]any{
		"get":    documentedJSONOperation("Get a webhook subscription", parameters, nil, "200", "Webhook", schemaRef("Webhook"), "401", "404", "500", "503"),
		"delete": documentedJSONOperation("Delete a webhook subscription", parameters, nil, "200", "Deleted webhook", objectSchema(map[string]any{"status": map[string]any{"type": "string"}}, "status"), "401", "404", "500", "503"),
	}
}

func pathWebhookDeliveries() map[string]any {
	return map[string]any{"get": documentedJSONOperation("List durable webhook deliveries", []map[string]any{pathIDParameter("id", "Webhook identifier")}, nil,
		"200", "Webhook deliveries", arraySchema(schemaRef("WebhookDelivery")), "401", "404", "500", "503")}
}

func pathWebhookDLQ() map[string]any {
	return map[string]any{"get": documentedJSONOperation("List undelivered webhook DLQ entries", nil, nil,
		"200", "Webhook DLQ entries", arraySchema(schemaRef("DLQEntry")), "401", "500", "503")}
}

func pathWebhookReprocess() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Reprocess a webhook DLQ entry", []map[string]any{pathIDParameter("id", "DLQ entry identifier")}, nil,
		"200", "Webhook reprocess result", schemaRef("WebhookReprocessResponse"), "401", "404", "500", "503")}
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

// pathTransactionListCreate documents the customer-scoped transaction list
// contract. The handler rejects requests without customer_id, so generated
// clients must expose it as a required query parameter.
func pathTransactionListCreate() map[string]any {
	params := append([]map[string]any{}, paginationParams()...)
	params = append(params, map[string]any{
		"name":        "customer_id",
		"in":          "query",
		"required":    true,
		"description": "Customer whose transactions are listed",
		"schema":      map[string]any{"type": "string"},
	})
	return map[string]any{
		"get": map[string]any{
			"summary":    "List Transactions",
			"parameters": params,
			"responses":  paginatedListResponses(),
		},
		"post": map[string]any{"summary": "Create Transaction", "responses": defaultResponses()},
	}
}

func pathGetPut(resource string) map[string]any {
	return map[string]any{
		"get": map[string]any{"summary": "Get " + resource, "responses": defaultResponses()},
		"put": map[string]any{"summary": "Update " + resource, "responses": defaultResponses()},
	}
}

func pathGetPutPatch(resource string) map[string]any {
	return map[string]any{
		"get":   map[string]any{"summary": "Get " + resource, "responses": defaultResponses()},
		"put":   map[string]any{"summary": "Update " + resource, "responses": defaultResponses()},
		"patch": map[string]any{"summary": "Partially update " + resource, "responses": defaultResponses()},
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

func pathPUT(summary string) map[string]any {
	return map[string]any{
		"put": map[string]any{"summary": summary, "responses": defaultResponses()},
	}
}

func pathPATCH(summary string) map[string]any {
	return map[string]any{
		"patch": map[string]any{"summary": summary, "responses": defaultResponses()},
	}
}

func pathRelationshipMutation() map[string]any {
	return map[string]any{
		"put":    map[string]any{"summary": "Correct related case link", "responses": defaultResponses()},
		"delete": map[string]any{"summary": "Remove related case link", "responses": defaultResponses()},
	}
}

func pathCaseTimeline() map[string]any {
	params := paginationParams()
	params = append(params,
		map[string]any{
			"name":        "event_type",
			"in":          "query",
			"description": "Filter by one event type; the parameter may be repeated or comma-separated",
			"schema":      map[string]any{"type": "string"},
		},
		map[string]any{
			"name":        "event_types",
			"in":          "query",
			"description": "Filter by comma-separated event types",
			"schema":      map[string]any{"type": "string"},
		},
		map[string]any{
			"name":        "include_inactive",
			"in":          "query",
			"description": "Include removed/corrected related-case history",
			"schema":      map[string]any{"type": "boolean", "default": false},
		},
	)
	return map[string]any{
		"get": map[string]any{
			"summary":    "Get bounded case timeline pages and investigation file sections",
			"parameters": append([]map[string]any{pathIDParameter("id", "Case identifier")}, params...),
			"responses":  successWithErrors("200", "Case investigation file", schemaRef("CaseFile"), "400", "401", "404", "409", "500", "503"),
		},
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
