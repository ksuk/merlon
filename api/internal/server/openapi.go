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
	for name, schema := range wave3Schemas() {
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
			"/healthz":                                                    pathProbeGET("Health check", true),
			"/healthz/live":                                               pathProbeGET("Liveness probe", false),
			"/healthz/ready":                                              pathProbeGET("Readiness probe", true),
			"/api/v1/customers":                                           pathCustomers(),
			"/api/v1/customers/{id}":                                      pathCustomer(),
			"/api/v1/customers/{id}/score":                                pathScoreCustomer(),
			"/api/v1/customers/{id}/screen":                               pathScreenCustomer(),
			"/api/v1/customers/{id}/scores":                               pathScoreHistory(),
			"/api/v1/customers/{id}/score-explanation":                    pathCustomerScoreExplanation(),
			"/api/v1/customers/{id}/scores/{scoreID}/explanation":         pathCustomerScoreExplanationByID(),
			"/api/v1/customers/{id}/screening-results":                    pathCustomerScreeningResults(),
			"/api/v1/customers/{id}/cdd-rule-sets":                        pathCustomerCDDRuleSets(),
			"/api/v1/customers/{id}/score-overrides":                      pathCustomerScoreOverrides(),
			"/api/v1/customers/{id}/score-overrides/{overrideID}/approve": pathCustomerScoreOverrideApprove(),
			"/api/v1/customers/{id}/edd/{action}":                         pathCustomerEDDAction(),
			"/api/v1/customers/{id}/edd-events":                           pathCustomerEDDEvents(),
			"/api/v1/customers/{id}/investigation":                        pathCustomerInvestigation(),
			"/api/v1/customers/{id}/identity-history":                     pathCustomerIdentityHistory(),
			"/api/v1/transactions":                                        pathTransactionListCreate(),
			"/api/v1/transactions/{id}":                                   pathTransactionGet(),
			"/api/v1/alerts":                                              pathListRiskPaginated("List alerts"),
			"/api/v1/alerts/{id}":                                         pathAlert(),
			"/api/v1/alerts/bulk-close":                                   pathBulkCloseAlerts(),
			"/api/v1/alerts/bulk-case":                                    pathBulkCaseAssignment(),
			"/api/v1/alerts/{id}/decisions":                               pathAlertDecisionHistory(),
			"/api/v1/backtest":                                            pathPOST("Run backtest"),
			"/api/v1/backtests":                                           pathBacktests(),
			"/api/v1/backtests/{id}":                                      pathBacktestJob(),
			"/api/v1/backtests/{id}/cancel":                               pathPOST("Cancel durable backtest job"),
			"/api/v1/backtests/{id}/affected-customers":                   pathBacktestAffectedCustomers(),
			"/api/v1/backtests/preview":                                   pathPOST("Preview the customer and transaction cohort a backtest would run over"),
			"/api/v1/backtests/rules":                                     pathBacktestRules(),
			"/api/v1/reports/str":                                         pathSTRReports(),
			"/api/v1/reports/str/{id}":                                    pathSTRReport(),
			"/api/v1/reports/str/{id}/submit":                             pathSubmitSTRReport(),
			"/api/v1/reports/str/export":                                  pathExportSTRReport(),
			"/api/v1/cases":                                               pathCases(),
			"/api/v1/cases/{id}":                                          pathCase(),
			"/api/v1/cases/{id}/notes":                                    pathCaseNote(),
			"/api/v1/cases/{id}/timeline":                                 pathCaseTimeline(),
			"/api/v1/cases/{id}/export":                                   pathExportCaseFile(),
			"/api/v1/cases/{id}/evidence":                                 pathAddCaseEvidence(),
			"/api/v1/cases/{id}/evidence/{evidence}/corrections":          pathCorrectCaseEvidence(),
			"/api/v1/cases/{id}/checklist/{item}":                         pathUpdateCaseChecklist(),
			"/api/v1/cases/{id}/work-items":                               pathCreateCaseWorkItem(),
			"/api/v1/cases/{id}/work-items/{item}":                        pathUpdateCaseWorkItem(),
			"/api/v1/cases/{id}/related":                                  pathRelatedCases(),
			"/api/v1/cases/{id}/related/{relationship}":                   pathRelatedCaseMutation(),
			"/api/v1/dashboard":                                           pathGET("Dashboard statistics"),
			"/api/v1/batch/score":                                         pathPOST("Batch score customers"),
			"/api/v1/batch/monitor":                                       pathPOST("Batch monitor transactions"),
			"/api/v1/batch/targets/preview":                               pathTargetPreview(),
			"/api/v1/batch/targets/{id}":                                  pathTargetManifest(),
			"/api/v1/batch/targets/{id}/confirm":                          pathTargetConfirmation(),
			"/api/v1/batch/runs":                                          pathBatchRuns(),
			"/api/v1/batch/runs/{id}":                                     pathBatchRun(),
			"/api/v1/batch/runs/{id}/cancel":                              pathBatchRunCancel(),
			"/api/v1/batch/runs/{id}/rerun":                               pathBatchRerun(),
			"/api/v1/pending-evaluations":                                 pathPendingEvaluations(),
			"/api/v1/pending-evaluations/export":                          pathPendingEvaluationExport(),
			"/api/v1/pending-evaluations/{id}":                            pathPendingEvaluation(),
			"/api/v1/pending-evaluations/{id}/history":                    pathPendingHistory(),
			"/api/v1/pending-evaluations/{id}/{action}":                   pathPendingTransition(),
			"/api/v1/webhooks/inbound/customer-status":                    pathCustomerStatusWebhook(),
			"/api/v1/webhooks":                                            pathWebhooks(),
			"/api/v1/webhooks/{id}":                                       pathWebhook(),
			"/api/v1/webhooks/{id}/deliveries":                            pathWebhookDeliveries(),
			"/api/v1/webhooks/dlq":                                        pathWebhookDLQ(),
			"/api/v1/webhooks/dlq/{id}/reprocess":                         pathWebhookReprocess(),
			"/api/v1/admin/apikeys":                                       pathCRUD("API Key", "apikeys"),
			"/api/v1/admin/apikeys/{id}":                                  pathDELETE("Revoke API key"),
			"/api/v1/operators":                                           pathOperatorDirectory(),
			"/api/v1/audit":                                               pathAuditLogs(),
			"/api/v1/audit/export":                                        pathExportAuditLogs(),
			"/api/v1/system/config-digests":                               pathGET("Get loaded configuration digests"),
			"/api/v1/policies":                                            pathPolicies(),
			"/api/v1/policies/{policy}":                                   pathPolicy(),
			"/api/v1/accounts":                                            pathAccountCreate(),
			"/api/v1/accounts/{id}":                                       pathAccountGet(),
			"/api/v1/accounts/{id}/customers":                             pathAccountCustomers(),
			"/api/v1/admin/retention-policies":                            pathRetentionPolicies(),
			"/api/v1/admin/retention-policies/{category}":                 pathRetentionPolicy(),
			"/api/v1/admin/users":                                         pathUsers(),
			"/api/v1/auth/login":                                          pathLogin(),
			"/api/v1/auth/logout":                                         pathLogout(),
			"/api/v1/auth/refresh":                                        pathRefresh(),
			"/api/v1/auth/me":                                             pathMe(),
			"/api/v1/config/validate":                                     pathValidateConfig(),
			"/api/v1/openapi.json":                                        pathOpenAPIDocument(),
			"/api/v1/rules":                                               pathRules(),
			"/api/v1/rules/{id}":                                          pathRule(),
			"/api/v1/rules/{id}/export":                                   pathRuleExport(),
			"/api/v1/rules/{id}/activate":                                 pathRuleActivation("activate"),
			"/api/v1/rules/{id}/deactivate":                               pathRuleActivation("deactivate"),
			"/api/v1/rules/import":                                        pathRuleImport(),
			"/api/v1/screening/check":                                     pathScreeningCheck(),
			"/api/v1/screening/runs":                                      pathScreeningRuns(),
			"/api/v1/screening/runs/{id}":                                 pathScreeningRun(),
			"/api/v1/screening/results":                                   pathScreeningResults(),
			"/api/v1/screening/results/{id}":                              pathScreeningResult(),
			"/api/v1/screening/results/{id}/history":                      pathScreeningResultHistory(),
			"/api/v1/screening/sources":                                   pathScreeningSources(),
			"/api/v1/setup":                                               pathSetup(),
			"/api/v1/system/info":                                         pathSystemInfo(),
			"/api/v1/whitelist":                                           pathWhitelist(),
			"/api/v1/whitelist/{id}":                                      pathWhitelistEntry(),
			"/api/v1/whitelist/{id}/approve":                              pathWhitelistApprove(),
			"/api/v1/whitelist/{id}/reviews":                              pathWhitelistReview(),
			"/api/v1/whitelist/{id}/revoke":                               pathWhitelistRevoke(),
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
			"customer_type": map[string]any{"type": "string", "enum": []string{"individual", "corporate_domestic", "corporate_foreign", "trust", "partnership", "npo", "government", "foreign_legal_arrangement"}}, "country_code": map[string]any{"type": "string"},
			"product_types": arraySchema(map[string]any{"type": "string"}), "attributes": map[string]any{"type": "object", "additionalProperties": true},
			"status":     map[string]any{"type": "string", "enum": []string{"active", "dormant", "frozen", "closed"}},
			"risk_score": map[string]any{"type": "number", "nullable": true}, "risk_tier": map[string]any{"type": "string", "nullable": true},
			"last_scored_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"created_at":     map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
			"kyc_missing_fields": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Required identity attributes this record does not carry, per the kyc_required_fields policy for its customer type. Recomputed on read; absent when nothing is missing."},
			"kyc_policy_version": map[string]any{"type": "string", "description": "Version of the policy that produced kyc_missing_fields"},
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

// wave3Schemas documents the durable workflow contracts added in Wave 3.
func wave3Schemas() map[string]any {
	return map[string]any{
		"CustomerCreateRequest": objectSchema(map[string]any{
			"external_id":   map[string]any{"type": "string"},
			"customer_type": map[string]any{"type": "string", "enum": []string{"individual", "corporate_domestic", "corporate_foreign", "trust", "partnership", "npo", "government", "foreign_legal_arrangement"}},
			"country_code":  map[string]any{"type": "string", "minLength": 2, "maxLength": 2},
			"product_types": arraySchema(map[string]any{"type": "string"}),
			"attributes":    map[string]any{"type": "object", "additionalProperties": true},
			"identity":      map[string]any{"type": "object", "additionalProperties": true, "description": "Configured KYC identity fields; unspecified legacy attributes are preserved"},
		}, "external_id", "customer_type", "country_code", "product_types", "attributes"),
		"CustomerUpdateRequest": objectSchema(map[string]any{
			"country_code":        map[string]any{"type": "string", "minLength": 2, "maxLength": 2},
			"status":              map[string]any{"type": "string", "enum": []string{"active", "dormant", "frozen", "closed"}},
			"product_types":       arraySchema(map[string]any{"type": "string"}),
			"attributes":          map[string]any{"type": "object", "additionalProperties": true},
			"identity":            map[string]any{"type": "object", "additionalProperties": true},
			"rationale":           map[string]any{"type": "string"},
			"expected_updated_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"PaginatedCustomers": objectSchema(map[string]any{"data": arraySchema(schemaRef("Customer")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"BacktestCreateRequest": objectSchema(map[string]any{
			"from": map[string]any{"type": "string", "format": "date-time"}, "to": map[string]any{"type": "string", "format": "date-time"},
			"customer_ids": arraySchema(map[string]any{"type": "string"}), "customer_filter": map[string]any{"type": "object", "additionalProperties": true},
			"scenario_ids": arraySchema(map[string]any{"type": "string"}), "baseline_rule_set_id": map[string]any{"type": "string"}, "candidate_rule_set_id": map[string]any{"type": "string"},
			"rationale": map[string]any{"type": "string"}, "rerun_of": map[string]any{"type": "string"},
		}, "from", "to", "candidate_rule_set_id"),
		"BacktestScenarioResult": objectSchema(map[string]any{
			"scenario_id": map[string]any{"type": "string"}, "alerts_generated": map[string]any{"type": "integer"}, "high_severity_count": map[string]any{"type": "integer"}, "medium_severity_count": map[string]any{"type": "integer"}, "low_severity_count": map[string]any{"type": "integer"}, "affected_customer_ids": arraySchema(map[string]any{"type": "string"}), "added_customer_ids": arraySchema(map[string]any{"type": "string"}), "removed_customer_ids": arraySchema(map[string]any{"type": "string"}),
		}, "scenario_id", "alerts_generated", "high_severity_count", "medium_severity_count", "low_severity_count", "affected_customer_ids"),
		"BacktestResult": objectSchema(map[string]any{"backtest_id": map[string]any{"type": "string"}, "total_transactions": map[string]any{"type": "integer"}, "total_customers": map[string]any{"type": "integer"}, "total_alerts": map[string]any{"type": "integer"}, "scenario_results": arraySchema(schemaRef("BacktestScenarioResult")), "execution_time_ms": map[string]any{"type": "number"}}, "backtest_id", "total_transactions", "total_customers", "total_alerts", "scenario_results", "execution_time_ms"),
		"BacktestJob": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"queued", "running", "completed", "failed", "cancelled"}}, "from": map[string]any{"type": "string", "format": "date-time"}, "to": map[string]any{"type": "string", "format": "date-time"}, "customer_ids": arraySchema(map[string]any{"type": "string"}), "customer_filter": map[string]any{"type": "object", "additionalProperties": true}, "scenario_ids": arraySchema(map[string]any{"type": "string"}), "baseline_rule_set_id": map[string]any{"type": "string"}, "candidate_rule_set_id": map[string]any{"type": "string"}, "baseline_rule_version": map[string]any{"type": "integer"}, "candidate_rule_version": map[string]any{"type": "integer"}, "config_digests": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, "snapshot_at": map[string]any{"type": "string", "format": "date-time"}, "total_customers": map[string]any{"type": "integer"}, "processed_customers": map[string]any{"type": "integer"}, "progress": map[string]any{"type": "number"}, "baseline": schemaRef("BacktestResult"), "candidate": schemaRef("BacktestResult"), "delta": schemaRef("BacktestResult"), "error": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"}, "started_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "completed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "updated_at": map[string]any{"type": "string", "format": "date-time"}, "metadata": schemaRef("BacktestMetadata"),
		}, "id", "status", "from", "to", "baseline_rule_set_id", "candidate_rule_set_id", "snapshot_at", "progress", "created_at", "updated_at"),
		"PaginatedBacktestJobs": objectSchema(map[string]any{"data": arraySchema(schemaRef("BacktestJob")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"BacktestAffectedCustomer": objectSchema(map[string]any{
			"job_id": map[string]any{"type": "string"}, "scenario_id": map[string]any{"type": "string"},
			"customer_id": map[string]any{"type": "string"},
			"delta_kind":  map[string]any{"type": "string", "enum": []string{"added", "removed", "unchanged"}, "description": "Whether the candidate rule set starts alerting on this customer, stops, or changes nothing"},
		}, "job_id", "scenario_id", "customer_id", "delta_kind"),
		"PaginatedAffectedBacktestCustomers": objectSchema(map[string]any{
			"data":        arraySchema(map[string]any{"type": "string"}),
			"rows":        arraySchema(schemaRef("BacktestAffectedCustomer")),
			"delta_kinds": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string", "enum": []string{"added", "removed", "unchanged", "mixed"}}, "description": "Per-customer verdict aggregated across scenarios; mixed means added by one scenario and removed by another"},
			"pagination":  schemaRef("PaginationMeta"),
		}, "data", "pagination"),
		"ScreenMatch":            objectSchema(map[string]any{"list_id": map[string]any{"type": "string"}, "entry_id": map[string]any{"type": "string"}, "matched_name": map[string]any{"type": "string"}, "similarity": map[string]any{"type": "number"}, "list_type": map[string]any{"type": "string"}, "source": map[string]any{"type": "string"}}, "list_id", "entry_id", "matched_name", "similarity", "list_type", "source"),
		"ScreenResult":           objectSchema(map[string]any{"customer_id": map[string]any{"type": "string"}, "hit": map[string]any{"type": "boolean"}, "matches": arraySchema(schemaRef("ScreenMatch")), "lists_checked": map[string]any{"type": "integer"}, "screened_at": map[string]any{"type": "string", "format": "date-time"}, "run_id": map[string]any{"type": "string"}, "result_ids": arraySchema(map[string]any{"type": "string"})}, "customer_id", "hit", "matches", "lists_checked", "screened_at"),
		"ScreeningBatchOutcome":  objectSchema(map[string]any{"customer_id": map[string]any{"type": "string"}, "screened": map[string]any{"type": "boolean"}, "skipped": map[string]any{"type": "boolean"}, "skip_reason": map[string]any{"type": "string"}, "error": map[string]any{"type": "string"}}, "customer_id", "screened", "skipped"),
		"ScreeningBatchResponse": objectSchema(map[string]any{"trigger": map[string]any{"type": "string"}, "outcomes": arraySchema(schemaRef("ScreeningBatchOutcome"))}, "trigger", "outcomes"),
		"ScoreCustomerRequest":   objectSchema(map[string]any{"rule_set_id": map[string]any{"type": "string"}, "rule_set_version": map[string]any{"type": "integer", "minimum": 1}, "rationale": map[string]any{"type": "string"}, "override_evidence": map[string]any{"type": "object", "additionalProperties": true}, "confirmed": map[string]any{"type": "boolean"}}, "rule_set_id"),
		"ScreeningRun": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "list_ids": arraySchema(map[string]any{"type": "string"}),
			"config_digests": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, "status": map[string]any{"type": "string", "enum": []string{"running", "completed", "failed", "partial"}}, "result_count": map[string]any{"type": "integer"},
			"error": map[string]any{"type": "string"}, "actor": map[string]any{"type": "string"}, "degraded": map[string]any{"type": "boolean", "description": "A required watchlist source was not ready when this run started"}, "degraded_sources": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "started_at": map[string]any{"type": "string", "format": "date-time"}, "completed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "created_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "customer_id", "list_ids", "config_digests", "status", "result_count", "actor", "started_at", "created_at"),
		"ScreeningResult": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "list_id": map[string]any{"type": "string"}, "list_type": map[string]any{"type": "string"}, "entry_id": map[string]any{"type": "string"}, "matched_name": map[string]any{"type": "string"}, "similarity": map[string]any{"type": "number"},
			"status": map[string]any{"type": "string", "enum": []string{"NEW", "REVIEWING", "TRUE_POSITIVE", "FALSE_POSITIVE"}}, "false_positive_reason": map[string]any{"type": "string"}, "reviewed_by": map[string]any{"type": "string"}, "reviewed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "screened_at": map[string]any{"type": "string", "format": "date-time"}, "created_at": map[string]any{"type": "string", "format": "date-time"},
			"run_id": map[string]any{"type": "string"}, "suppressed": map[string]any{"type": "boolean"}, "suppression_reason": map[string]any{"type": "string"}, "degraded": map[string]any{"type": "boolean", "description": "A required watchlist source was not ready when this run started"}, "degraded_sources": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "match_evidence": map[string]any{"type": "object", "additionalProperties": true}, "case_id": map[string]any{"type": "string"}, "version": map[string]any{"type": "integer"}, "updated_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "customer_id", "list_id", "status", "screened_at", "created_at", "suppressed", "version", "updated_at"),
		"ScreeningResultHistory": objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "screening_result_id": map[string]any{"type": "string"}, "from_status": map[string]any{"type": "string"}, "to_status": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}, "actor": map[string]any{"type": "string"}, "version": map[string]any{"type": "integer"}, "created_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "screening_result_id", "from_status", "to_status", "rationale", "actor", "version", "created_at"),
		"ScreeningSourceStatus":  objectSchema(map[string]any{"list_id": map[string]any{"type": "string"}, "list_type": map[string]any{"type": "string"}, "configured": map[string]any{"type": "boolean"}, "operational_state": map[string]any{"type": "string", "enum": []string{"never_imported", "ready", "stale", "unreadable", "failed", "unavailable"}}, "last_attempt_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "last_failure_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "last_success_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "age_seconds": map[string]any{"type": "integer", "nullable": true}, "freshness_threshold_seconds": map[string]any{"type": "integer"}, "consecutive_failures": map[string]any{"type": "integer"}, "diagnostic": map[string]any{"type": "string"}}, "list_id", "list_type", "configured", "operational_state", "freshness_threshold_seconds", "consecutive_failures"),
		"PolicyDescriptor": objectSchema(map[string]any{
			"name":           map[string]any{"type": "string", "enum": []string{"kyc_required_fields", "edd", "cdd_rule_selection", "travel_rule", "screening_readiness"}},
			"schema_version": map[string]any{"type": "string"},
			"policy_version": map[string]any{"type": "string"},
			"digest":         map[string]any{"type": "string", "description": "SHA-256 of the policy document, pinned onto the decisions it produced"},
			"source":         map[string]any{"type": "string", "enum": []string{"file", "default"}},
		}, "name", "schema_version", "policy_version", "digest", "source"),
		"PolicyList": objectSchema(map[string]any{"data": arraySchema(schemaRef("PolicyDescriptor"))}, "data"),
		"PolicyDocument": objectSchema(map[string]any{
			"name":           map[string]any{"type": "string"},
			"schema_version": map[string]any{"type": "string"},
			"policy_version": map[string]any{"type": "string"},
			"digest":         map[string]any{"type": "string"},
			"source":         map[string]any{"type": "string", "enum": []string{"file", "default"}},
			"document":       map[string]any{"type": "object", "description": "The policy document as authored; its shape is governed by schema_version", "additionalProperties": true},
		}, "name", "schema_version", "policy_version", "digest", "source", "document"),
		"ScreeningSourceDirectory": objectSchema(map[string]any{
			"data": arraySchema(schemaRef("ScreeningSourceStatus")), "configured_count": map[string]any{"type": "integer"},
			"ready_count": map[string]any{"type": "integer"}, "unready_count": map[string]any{"type": "integer"},
			"screening_ready":  map[string]any{"type": "boolean", "description": "Every source the readiness policy marks required is usable"},
			"degraded_sources": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Required sources that are not ready"},
			"policy_version":   map[string]any{"type": "string"},
		}, "data", "configured_count", "ready_count", "unready_count", "screening_ready"),
		"ScreeningReviewRequest":    objectSchema(map[string]any{"status": map[string]any{"type": "string", "enum": []string{"REVIEWING", "TRUE_POSITIVE", "FALSE_POSITIVE"}}, "false_positive_reason": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}, "expected_version": map[string]any{"type": "integer", "minimum": 1}}, "status", "expected_version"),
		"ScreeningReviewOutcome":    objectSchema(map[string]any{"result": schemaRef("ScreeningResult"), "case_id": map[string]any{"type": "string"}, "case_created": map[string]any{"type": "boolean"}}, "result", "case_created"),
		"BacktestMetadata":          objectSchema(map[string]any{"job_id": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}, "cohort_preview": map[string]any{"type": "object", "additionalProperties": true}, "baseline_snapshot": map[string]any{"type": "object", "additionalProperties": true}, "candidate_snapshot": map[string]any{"type": "object", "additionalProperties": true}, "rerun_of": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"}}, "job_id", "rationale", "cohort_preview", "baseline_snapshot", "candidate_snapshot", "created_at"),
		"TargetManifest":            objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "operation": map[string]any{"type": "string"}, "target_mode": map[string]any{"type": "string", "enum": []string{"selected", "filter", "all"}}, "customer_ids": arraySchema(map[string]any{"type": "string"}), "filter": map[string]any{"type": "object", "additionalProperties": true}, "sample_customer_ids": arraySchema(map[string]any{"type": "string"}), "target_count": map[string]any{"type": "integer"}, "criteria": map[string]any{"type": "string"}, "rule_set_id": map[string]any{"type": "string"}, "rule_set_version": map[string]any{"type": "integer"}, "config_digests": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, "token": map[string]any{"type": "string", "writeOnly": true}, "idempotency_key": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"preview", "confirmed", "consumed", "expired"}}, "version": map[string]any{"type": "integer"}, "expires_at": map[string]any{"type": "string", "format": "date-time"}, "created_by": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"}, "confirmed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "run_id": map[string]any{"type": "string"}}, "id", "operation", "target_mode", "customer_ids", "sample_customer_ids", "target_count", "criteria", "status", "version", "expires_at", "created_by", "created_at"),
		"TargetPreviewRequest":      objectSchema(map[string]any{"operation": map[string]any{"type": "string"}, "target_mode": map[string]any{"type": "string", "enum": []string{"selected", "filter", "all"}}, "customer_ids": arraySchema(map[string]any{"type": "string"}), "filter": map[string]any{"type": "object", "additionalProperties": true}, "criteria": map[string]any{"type": "string"}, "rule_set_id": map[string]any{"type": "string"}, "rule_set_version": map[string]any{"type": "integer"}, "rationale": map[string]any{"type": "string"}, "idempotency_key": map[string]any{"type": "string"}, "ttl_seconds": map[string]any{"type": "integer"}}, "target_mode"),
		"TargetConfirmationRequest": objectSchema(map[string]any{"token": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}, "idempotency_key": map[string]any{"type": "string"}, "expected_version": map[string]any{"type": "integer", "minimum": 1}}, "token", "rationale", "expected_version"),
		"BatchRun":                  objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "job_type": map[string]any{"type": "string"}, "operation": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"running", "completed", "failed", "partial", "cancelled"}}, "parameters": map[string]any{"type": "object", "additionalProperties": true}, "target_manifest_id": map[string]any{"type": "string"}, "config_digests": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, "actor": map[string]any{"type": "string"}, "result_counts": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer"}}, "customer_outcomes": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object", "additionalProperties": true}}, "error": map[string]any{"type": "string"}, "rerun_of": map[string]any{"type": "string"}, "started_at": map[string]any{"type": "string", "format": "date-time"}, "completed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "updated_at": map[string]any{"type": "string", "format": "date-time"}, "processed_customer_ids": arraySchema(map[string]any{"type": "string"})}, "id", "job_type", "status", "started_at", "processed_customer_ids"),
		"BatchRunCreateRequest":     objectSchema(map[string]any{"operation": map[string]any{"type": "string"}, "target_manifest_id": map[string]any{"type": "string"}, "parameters": map[string]any{"type": "object", "additionalProperties": true}, "rationale": map[string]any{"type": "string"}, "idempotency_key": map[string]any{"type": "string"}, "rerun_of": map[string]any{"type": "string"}}, "operation", "target_manifest_id"),
		"BatchRunCancelRequest":     objectSchema(map[string]any{"reason": map[string]any{"type": "string", "description": "Why the operator stopped the run; recorded on the run and in the audit log"}}),
		"BatchRerunPreview": objectSchema(map[string]any{
			"target_manifest": schemaRef("TargetManifest"),
			"operation":       map[string]any{"type": "string"},
			"parameters":      map[string]any{"type": "object", "additionalProperties": true},
			"rerun_of":        map[string]any{"type": "string"},
			"next":            map[string]any{"type": "string", "description": "The confirmation step required before the rerun can start"},
		}, "target_manifest", "operation", "rerun_of"),
		"CDDScoreOverride": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"},
			"score_record_id": map[string]any{"type": "string"},
			"proposed_tier":   map[string]any{"type": "string"}, "computed_tier": map[string]any{"type": "string"},
			"computed_score": map[string]any{"type": "number"}, "reason": map[string]any{"type": "string"},
			"supporting_documents": arraySchema(map[string]any{"type": "string"}),
			"evidence":             map[string]any{"type": "object", "additionalProperties": true},
			"status":               map[string]any{"type": "string", "enum": []string{"pending_approval", "approved", "rejected"}},
			"requested_by":         map[string]any{"type": "string"}, "requested_at": map[string]any{"type": "string", "format": "date-time"},
			"decided_by": map[string]any{"type": "string"}, "decided_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"decision_rationale": map[string]any{"type": "string"}, "version": map[string]any{"type": "integer"},
		}, "id", "customer_id", "proposed_tier", "computed_tier", "reason", "status", "requested_by", "requested_at", "version"),
		"CDDScoreOverrideDecision": objectSchema(map[string]any{
			"rationale":        map[string]any{"type": "string"},
			"reject":           map[string]any{"type": "boolean", "description": "Reject rather than approve the proposal"},
			"expected_version": map[string]any{"type": "integer"},
		}, "rationale", "expected_version"),
		"CDDRuleSetCandidates": objectSchema(map[string]any{
			"data": arraySchema(objectSchema(map[string]any{
				"id": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"},
				"version": map[string]any{"type": "integer"}, "is_active": map[string]any{"type": "boolean"},
				"digest": map[string]any{"type": "string"}, "matched_on": map[string]any{"type": "string"},
				"priority": map[string]any{"type": "integer"}, "recommended": map[string]any{"type": "boolean"},
			}, "id", "name", "version", "is_active", "digest", "recommended")),
			"policy_version":      map[string]any{"type": "string"},
			"selection_authority": map[string]any{"type": "boolean", "description": "True when the server, not the caller, chooses the rule set"},
		}, "data", "policy_version"),
		"EDDActionRequest": objectSchema(map[string]any{
			"rationale":           map[string]any{"type": "string", "description": "Why the window is being closed or reopened; required by the edd policy"},
			"case_id":             map[string]any{"type": "string"},
			"expected_updated_at": map[string]any{"type": "string", "format": "date-time"},
		}),
		"EDDPanel": objectSchema(map[string]any{
			"required": map[string]any{"type": "boolean"}, "requested_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"completed_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"closed_at":    map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"close_reason": map[string]any{"type": "string"}, "current_stage": map[string]any{"type": "string"},
			"elapsed_days":   map[string]any{"type": "integer"},
			"remaining_days": map[string]any{"type": "integer", "deprecated": true, "description": "Clamped at zero and therefore unable to express lateness; use overdue_days"},
			"overdue_days":   map[string]any{"type": "integer", "description": "Whole days past the due boundary, never negative"},
			"due_at":         map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"next_stage":     map[string]any{"type": "string"}, "next_stage_at": map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"completion_status": map[string]any{"type": "string", "enum": []string{"not_required", "open", "overdue", "escalated", "completed"}},
			"case_id":           map[string]any{"type": "string"}, "policy_version": map[string]any{"type": "string"},
		}, "required", "current_stage", "completion_status"),
		"EDDEvent": objectSchema(map[string]any{
			"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"},
			"event_type": map[string]any{"type": "string", "enum": []string{"requested", "stage_escalated", "completed", "reopened", "closed_on_downgrade"}},
			"stage":      map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"},
			"case_id": map[string]any{"type": "string"}, "actor": map[string]any{"type": "string"},
			"policy_version": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"},
		}, "id", "customer_id", "event_type", "actor", "created_at"),
		"PendingEvaluation":           objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "transaction_ids": arraySchema(map[string]any{"type": "string"}), "alert_ids": arraySchema(map[string]any{"type": "string"}), "status": map[string]any{"type": "string", "enum": []string{"PENDING_REVIEW", "PROCESSING", "RESOLVED", "FAILED"}}, "reason": map[string]any{"type": "string"}, "batch_run_id": map[string]any{"type": "string", "nullable": true}, "retry_count": map[string]any{"type": "integer"}, "manual_retry_count": map[string]any{"type": "integer", "description": "Operator-initiated revivals of a failed record, counted separately from the automatic retry budget"}, "resolved_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "last_attempt_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "next_retry_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "escalated_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "version": map[string]any{"type": "integer"}, "created_at": map[string]any{"type": "string", "format": "date-time"}, "updated_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "customer_id", "transaction_ids", "status", "reason", "retry_count", "version", "created_at", "updated_at"),
		"PendingTransitionRequest":    objectSchema(map[string]any{"reason": map[string]any{"type": "string"}, "expected_version": map[string]any{"type": "integer", "minimum": 1}}, "expected_version"),
		"PendingEvaluationHistory":    objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "pending_evaluation_id": map[string]any{"type": "string"}, "from_status": map[string]any{"type": "string"}, "to_status": map[string]any{"type": "string"}, "action": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}, "actor": map[string]any{"type": "string"}, "retry_count": map[string]any{"type": "integer"}, "created_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "pending_evaluation_id", "from_status", "to_status", "action", "actor", "retry_count", "created_at"),
		"CustomerIdentityHistory":     objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "changed_fields": map[string]any{"type": "object", "additionalProperties": true}, "actor": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "customer_id", "changed_fields", "actor", "rationale", "created_at"),
		"Factor":                      objectSchema(map[string]any{"name": map[string]any{"type": "string"}, "axis": map[string]any{"type": "string"}, "score": map[string]any{"type": "number"}, "description": map[string]any{"type": "string"}, "business_meaning": map[string]any{"type": "string"}, "weight": map[string]any{"type": "number"}, "contribution": map[string]any{"type": "number"}, "observed_value": map[string]any{"type": "string"}, "rule": map[string]any{"type": "string"}, "fallback": map[string]any{"type": "boolean"}}, "name", "axis", "score", "description"),
		"ScoreRecord":                 objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "rule_set_id": map[string]any{"type": "string"}, "rule_set_sha256": map[string]any{"type": "string"}, "score": map[string]any{"type": "number"}, "tier": map[string]any{"type": "string"}, "factors": arraySchema(schemaRef("Factor")), "rationale": map[string]any{"type": "string"}, "actor": map[string]any{"type": "string"}, "override_evidence": map[string]any{"type": "object", "additionalProperties": true}, "scored_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "customer_id", "score", "factors", "scored_at"),
		"ScoreExplanation":            objectSchema(map[string]any{"score": schemaRef("ScoreRecord"), "total_reconciled": map[string]any{"type": "number"}, "rule_set_id": map[string]any{"type": "string"}, "rule_set_sha256": map[string]any{"type": "string"}, "priority": map[string]any{"type": "string"}, "deterministic": map[string]any{"type": "boolean"}}, "score", "total_reconciled", "rule_set_id", "rule_set_sha256", "priority", "deterministic"),
		"InvestigationTimelineEntry":  objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string", "enum": []string{"transaction", "alert", "case", "screening_result", "score"}}, "entity_id": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"}, "created_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "kind", "entity_id", "summary", "created_at"),
		"InvestigationEDD":            objectSchema(map[string]any{"required": map[string]any{"type": "boolean"}, "requested_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "stage1_last_sent_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "stage2_notified_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "stage3_notified_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "current_stage": map[string]any{"type": "string", "enum": []string{"none", "requested", "stage1", "stage2", "critical"}}, "elapsed_days": map[string]any{"type": "integer"}, "remaining_days": map[string]any{"type": "integer"}, "next_stage": map[string]any{"type": "string", "enum": []string{"none", "stage1", "stage2", "stage3"}}, "next_stage_at": map[string]any{"type": "string", "format": "date-time", "nullable": true}, "completion_status": map[string]any{"type": "string", "enum": []string{"not_required", "open", "escalated"}}}, "required", "current_stage", "elapsed_days", "remaining_days", "next_stage", "completion_status"),
		"CustomerIdentityHistoryPage": objectSchema(map[string]any{"data": arraySchema(schemaRef("CustomerIdentityHistory")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"CustomerInvestigation":       objectSchema(map[string]any{"customer": schemaRef("Customer"), "counts": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer"}}, "pagination": map[string]any{"type": "object", "additionalProperties": schemaRef("PaginationMeta")}, "transactions": arraySchema(schemaRef("Transaction")), "alerts": arraySchema(schemaRef("Alert")), "cases": arraySchema(schemaRef("Case")), "screening_results": arraySchema(schemaRef("ScreeningResult")), "score_history": arraySchema(schemaRef("ScoreRecord")), "timeline": arraySchema(schemaRef("InvestigationTimelineEntry")), "edd": schemaRef("InvestigationEDD"), "freshness": map[string]any{"type": "string", "format": "date-time"}, "partial_failures": arraySchema(map[string]any{"type": "string"})}, "customer", "counts", "pagination", "transactions", "alerts", "cases", "screening_results", "score_history", "timeline", "edd", "freshness", "partial_failures"),
		"Transaction":                 objectSchema(map[string]any{"id": map[string]any{"type": "string"}, "customer_id": map[string]any{"type": "string"}, "external_id": map[string]any{"type": "string"}, "amount": map[string]any{"type": "number"}, "currency": map[string]any{"type": "string"}, "direction": map[string]any{"type": "string"}, "counterparty_id": map[string]any{"type": "string"}, "counterparty_country": map[string]any{"type": "string"}, "channel": map[string]any{"type": "string"}, "account_id": map[string]any{"type": "string", "nullable": true}, "counterparty": map[string]any{"type": "object", "additionalProperties": true, "nullable": true}, "metadata": map[string]any{"type": "object", "additionalProperties": true}, "idempotency_key": map[string]any{"type": "string", "nullable": true}, "travel_rule_applicable": map[string]any{"type": "boolean", "nullable": true}, "travel_rule_evidence": map[string]any{"type": "object", "additionalProperties": true}, "travel_rule_not_applicable_reason": map[string]any{"type": "string"}, "travel_rule_not_applicable_reason_code": map[string]any{"type": "string", "description": "Closed-enum companion to travel_rule_not_applicable_reason; free text with no code maps to other"}, "travel_rule_status": map[string]any{"type": "string", "enum": []string{"complete", "incomplete", "not_applicable"}, "description": "The server's own status, derived from the evidence present rather than from the client's assertion"}, "travel_rule_assessment": map[string]any{"type": "object", "additionalProperties": true, "nullable": true, "description": "The server's recorded verdict: policy_version, applicable, reason_code, missing_fields, threshold, currency, conflict, evaluated_at. Null on transactions accepted before the policy existed, which is neither not-applicable nor unknown."}, "executed_at": map[string]any{"type": "string", "format": "date-time"}, "created_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "customer_id", "external_id", "amount", "currency", "direction", "executed_at", "created_at"),
		"CreateTransactionRequest":    objectSchema(map[string]any{"customer_id": map[string]any{"type": "string"}, "external_id": map[string]any{"type": "string"}, "amount": map[string]any{"type": "number"}, "currency": map[string]any{"type": "string"}, "direction": map[string]any{"type": "string"}, "counterparty_id": map[string]any{"type": "string"}, "counterparty_country": map[string]any{"type": "string"}, "channel": map[string]any{"type": "string"}, "account_id": map[string]any{"type": "string"}, "counterparty": map[string]any{"type": "object", "additionalProperties": true}, "metadata": map[string]any{"type": "object", "additionalProperties": true}, "travel_rule_applicable": map[string]any{"type": "boolean"}, "travel_rule_evidence": map[string]any{"type": "object", "additionalProperties": true}, "travel_rule_not_applicable_reason": map[string]any{"type": "string"}, "travel_rule_not_applicable_reason_code": map[string]any{"type": "string", "description": "Closed-enum companion to travel_rule_not_applicable_reason; free text with no code maps to other"}, "travel_rule_status": map[string]any{"type": "string", "enum": []string{"complete", "incomplete", "not_applicable"}, "description": "The server's own status, derived from the evidence present rather than from the client's assertion"}, "travel_rule_assessment": map[string]any{"type": "object", "additionalProperties": true, "nullable": true, "description": "The server's recorded verdict: policy_version, applicable, reason_code, missing_fields, threshold, currency, conflict, evaluated_at. Null on transactions accepted before the policy existed, which is neither not-applicable nor unknown."}, "executed_at": map[string]any{"type": "string", "format": "date-time"}}, "customer_id", "external_id", "amount", "currency", "direction", "executed_at"),
		"PaginatedScreeningRuns":      objectSchema(map[string]any{"data": arraySchema(schemaRef("ScreeningRun")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"PaginatedScreeningResults":   objectSchema(map[string]any{"data": arraySchema(schemaRef("ScreeningResult")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"PaginatedPendingEvaluations": objectSchema(map[string]any{"data": arraySchema(schemaRef("PendingEvaluation")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"PaginatedBatchRuns":          objectSchema(map[string]any{"data": arraySchema(schemaRef("BatchRun")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
		"PaginatedTransactions":       objectSchema(map[string]any{"data": arraySchema(schemaRef("Transaction")), "pagination": schemaRef("PaginationMeta")}, "data", "pagination"),
	}
}

func publicOperation(operation map[string]any) map[string]any {
	operation["security"] = []map[string]any{}
	return operation
}

func wave3PageParams(extra ...map[string]any) []map[string]any {
	params := append([]map[string]any{}, paginationParams()...)
	return append(params, extra...)
}

func wave3PaginatedResponses(description string, item any) map[string]any {
	return successWithErrors("200", description, objectSchema(map[string]any{
		"data": arraySchema(item), "pagination": schemaRef("PaginationMeta"),
	}, "data", "pagination"), "400", "401", "404", "429", "500", "503")
}

func pathCustomerScoreExplanation() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Explain the latest CDD score", []map[string]any{pathIDParameter("id", "Customer identifier")}, nil, "200", "Explainable CDD score", schemaRef("ScoreExplanation"), "401", "404", "500", "503")}
}

func pathCustomerScoreExplanationByID() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Explain a selected CDD score", []map[string]any{pathIDParameter("id", "Customer identifier"), pathIDParameter("scoreID", "Score record identifier")}, nil, "200", "Explainable CDD score", schemaRef("ScoreExplanation"), "401", "404", "500", "503")}
}

func pathCustomerScreeningResults() map[string]any {
	params := wave3PageParams(
		map[string]any{"name": "list_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "status", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"NEW", "REVIEWING", "TRUE_POSITIVE", "FALSE_POSITIVE"}}},
	)
	return map[string]any{"get": map[string]any{"summary": "List durable screening results for a customer", "parameters": append([]map[string]any{pathIDParameter("id", "Customer identifier")}, params...), "responses": wave3PaginatedResponses("Screening results", schemaRef("ScreeningResult"))}}
}

func pathCustomerInvestigation() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get the customer 360 investigation read model", append([]map[string]any{pathIDParameter("id", "Customer identifier")}, paginationParams()...), nil, "200", "Customer investigation read model", schemaRef("CustomerInvestigation"), "400", "401", "404", "500", "503")}
}

func pathCustomerIdentityHistory() map[string]any {
	return map[string]any{"get": map[string]any{"summary": "List customer identity change history", "parameters": append([]map[string]any{pathIDParameter("id", "Customer identifier")}, paginationParams()...), "responses": wave3PaginatedResponses("Customer identity history", schemaRef("CustomerIdentityHistory"))}}
}

func pathScreeningRuns() map[string]any {
	return map[string]any{"get": map[string]any{"summary": "List durable screening runs", "parameters": append(paginationParams(), map[string]any{"name": "customer_id", "in": "query", "schema": map[string]any{"type": "string"}}), "responses": successWithErrors("200", "Screening runs", schemaRef("PaginatedScreeningRuns"), "400", "401", "429", "500", "503")}}
}

func pathScreeningRun() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get a durable screening run", []map[string]any{pathIDParameter("id", "Screening run identifier")}, nil, "200", "Screening run", schemaRef("ScreeningRun"), "401", "404", "500", "503")}
}

func screeningResultListParams() []map[string]any {
	return wave3PageParams(
		map[string]any{"name": "customer_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "list_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "status", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"NEW", "REVIEWING", "TRUE_POSITIVE", "FALSE_POSITIVE"}}},
		map[string]any{"name": "from", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
		map[string]any{"name": "to", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
		map[string]any{"name": "suppressed", "in": "query", "description": "Restrict to suppressed or unsuppressed hits; omitted returns both", "schema": map[string]any{"type": "boolean"}},
	)
}

func pathScreeningResults() map[string]any {
	return map[string]any{"get": map[string]any{"summary": "List durable screening results", "parameters": screeningResultListParams(), "responses": successWithErrors("200", "Screening results", schemaRef("PaginatedScreeningResults"), "400", "401", "429", "500", "503")}}
}

func pathScreeningResultHistory() map[string]any {
	return map[string]any{"get": documentedJSONOperation("List screening result decision history", []map[string]any{pathIDParameter("id", "Screening result identifier"), {"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 50, "maximum": 200}}}, nil, "200", "Screening result history", arraySchema(schemaRef("ScreeningResultHistory")), "400", "401", "404", "500", "503")}
}

func pathPolicies() map[string]any {
	return map[string]any{"get": documentedJSONOperation("List the loaded AML policy documents", nil, nil, "200", "Policy descriptors", schemaRef("PolicyList"), "401", "500")}
}

func pathPolicy() map[string]any {
	params := []map[string]any{{
		"name": "policy", "in": "path", "required": true, "description": "Policy document name",
		"schema": map[string]any{"type": "string", "enum": []string{"kyc_required_fields", "edd", "cdd_rule_selection", "travel_rule", "screening_readiness"}},
	}}
	return map[string]any{"get": documentedJSONOperation("Get one AML policy document", params, nil, "200", "Policy document", schemaRef("PolicyDocument"), "401", "404", "500")}
}

func pathScreeningSources() map[string]any {
	params := []map[string]any{
		{"name": "source_ids", "in": "query", "description": "Comma-separated configured source identifiers", "schema": map[string]any{"type": "string"}},
		{"name": "freshness_threshold_seconds", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1}},
	}
	return map[string]any{"get": documentedJSONOperation("List the configured screening source directory", params, nil, "200", "Screening source readiness", schemaRef("ScreeningSourceDirectory"), "400", "401", "500", "503")}
}

func pathBacktestRules() map[string]any {
	return map[string]any{"get": map[string]any{"summary": "Discover rule sets available for comparison", "parameters": paginationParams(), "responses": successWithErrors("200", "Backtest rule candidates", schemaRef("PaginatedRules"), "400", "401", "429", "500", "503")}}
}

func pathTargetPreview() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Preview and freeze a batch target manifest", nil, schemaRef("TargetPreviewRequest"), "201", "Target manifest preview", schemaRef("TargetManifest"), "400", "401", "409", "500", "503")}
}

func pathTargetManifest() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get a target manifest", []map[string]any{pathIDParameter("id", "Target manifest identifier")}, nil, "200", "Target manifest", schemaRef("TargetManifest"), "401", "404", "500", "503")}
}

func pathTargetConfirmation() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Confirm an immutable target manifest", []map[string]any{pathIDParameter("id", "Target manifest identifier")}, schemaRef("TargetConfirmationRequest"), "200", "Confirmed target manifest", schemaRef("TargetManifest"), "400", "401", "404", "409", "500", "503")}
}

func pathBatchRuns() map[string]any {
	return map[string]any{
		"get":  map[string]any{"summary": "List durable manual batch runs", "parameters": append([]map[string]any{}, paginationParams()...), "responses": successWithErrors("200", "Batch runs", schemaRef("PaginatedBatchRuns"), "400", "401", "429", "500", "503")},
		"post": documentedJSONOperation("Start a durable manual batch run", nil, schemaRef("BatchRunCreateRequest"), "202", "Batch run accepted; execution continues independently of this request", schemaRef("BatchRun"), "400", "401", "404", "409", "500", "503"),
	}
}

func pathBatchRun() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get a durable manual batch run", []map[string]any{pathIDParameter("id", "Batch run identifier")}, nil, "200", "Batch run", schemaRef("BatchRun"), "401", "404", "500", "503")}
}

func pathBatchRerun() map[string]any {
	return map[string]any{"post": documentedJSONOperation(
		"Prepare a rerun of a durable manual batch run",
		[]map[string]any{pathIDParameter("id", "Batch run identifier")}, nil,
		"201", "Unconfirmed target manifest for the rerun; confirm it before starting a run",
		schemaRef("BatchRerunPreview"), "401", "404", "409", "500", "503")}
}

func pathBatchRunCancel() map[string]any {
	return map[string]any{"post": documentedJSONOperation(
		"Cancel a running manual batch run",
		[]map[string]any{pathIDParameter("id", "Batch run identifier")},
		schemaRef("BatchRunCancelRequest"),
		"200", "Cancelled batch run; work already completed is retained",
		schemaRef("BatchRun"), "400", "401", "404", "409", "500", "503")}
}

func pathPendingEvaluations() map[string]any {
	params := wave3PageParams(
		map[string]any{"name": "status", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "customer_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "batch_run_id", "in": "query", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "created_from", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
		map[string]any{"name": "created_to", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
		map[string]any{"name": "min_age_days", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 0}},
		map[string]any{"name": "max_age_days", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 0}},
	)
	return map[string]any{"get": map[string]any{"summary": "List pending evaluation recovery items", "parameters": params, "responses": successWithErrors("200", "Pending evaluations", schemaRef("PaginatedPendingEvaluations"), "400", "401", "429", "500", "503")}}
}

func pathPendingEvaluation() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get a pending evaluation", []map[string]any{pathIDParameter("id", "Pending evaluation identifier")}, nil, "200", "Pending evaluation", schemaRef("PendingEvaluation"), "401", "404", "500", "503")}
}

func pathPendingHistory() map[string]any {
	return map[string]any{"get": documentedJSONOperation("List pending evaluation history", []map[string]any{pathIDParameter("id", "Pending evaluation identifier")}, nil, "200", "Pending evaluation history", arraySchema(schemaRef("PendingEvaluationHistory")), "401", "404", "500", "503")}
}

func pathPendingTransition() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Transition a pending evaluation", []map[string]any{pathIDParameter("id", "Pending evaluation identifier"), pathIDParameter("action", "retry, resolve, or escalate")}, schemaRef("PendingTransitionRequest"), "200", "Updated pending evaluation", schemaRef("PendingEvaluation"), "400", "401", "404", "409", "500", "503")}
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
		schemaRef("ScoreCustomerRequest"),
		"200", "Score record", schemaRef("ScoreRecord"), "400", "401", "404", "500", "502", "503")}
}

func pathScreenCustomer() map[string]any {
	return map[string]any{"post": documentedJSONOperation("Screen a customer against configured lists", []map[string]any{pathIDParameter("id", "Customer identifier")},
		objectSchema(map[string]any{"list_ids": arraySchema(map[string]any{"type": "string"})}),
		"200", "Screening result", schemaRef("ScreenResult"), "400", "401", "404", "500", "502", "503")}
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
		"200", "Screening batch result", schemaRef("ScreeningBatchResponse"), "400", "401", "404", "500", "503")}
}

func pathScreeningResult() map[string]any {
	params := []map[string]any{pathIDParameter("id", "Screening result identifier")}
	return map[string]any{
		"get":   documentedJSONOperation("Get a durable screening result", params, nil, "200", "Screening result", schemaRef("ScreeningResult"), "401", "404", "500", "503"),
		"patch": documentedJSONOperation("Review a screening result", params, schemaRef("ScreeningReviewRequest"), "200", "Updated screening result", schemaRef("ScreeningReviewOutcome"), "400", "401", "404", "409", "500", "503"),
	}
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

func pathCustomers() map[string]any {
	parameters := append([]map[string]any{}, paginationParams()...)
	parameters = append(parameters, map[string]any{"name": "search", "in": "query", "description": "Search external ID, identity name, kana, address, or country", "schema": map[string]any{"type": "string"}})
	return map[string]any{
		"get":  documentedJSONOperation("List customers", parameters, nil, "200", "Customers", schemaRef("PaginatedCustomers"), "400", "401", "429", "500", "503"),
		"post": documentedJSONOperation("Create customer KYC identity", nil, schemaRef("CustomerCreateRequest"), "201", "Created customer", schemaRef("Customer"), "400", "401", "409", "500", "503"),
	}
}

func pathCustomer() map[string]any {
	parameters := []map[string]any{pathIDParameter("id", "Customer identifier")}
	return map[string]any{
		"get": documentedJSONOperation("Get customer", parameters, nil, "200", "Customer", schemaRef("Customer"), "401", "404", "500"),
		"put": documentedJSONOperation("Partially update customer KYC identity", parameters, schemaRef("CustomerUpdateRequest"), "200", "Updated customer", schemaRef("Customer"), "400", "401", "404", "409", "500", "503"),
	}
}

func pathBacktests() map[string]any {
	return map[string]any{
		"get":  documentedJSONOperation("List durable backtest jobs", paginationParams(), nil, "200", "Backtest jobs", schemaRef("PaginatedBacktestJobs"), "400", "401", "429", "500", "503"),
		"post": documentedJSONOperation("Create a durable backtest comparison", nil, schemaRef("BacktestCreateRequest"), "202", "Queued backtest job", schemaRef("BacktestJob"), "400", "401", "409", "500", "503"),
	}
}

func pathBacktestJob() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get durable backtest job", []map[string]any{pathIDParameter("id", "Backtest job identifier")}, nil, "200", "Backtest job", schemaRef("BacktestJob"), "401", "404", "500", "503")}
}

func pathCustomerCDDRuleSets() map[string]any {
	return map[string]any{"get": documentedJSONOperation(
		"List the CDD rule sets applicable to a customer",
		[]map[string]any{pathIDParameter("id", "Customer identifier")}, nil,
		"200", "Rule-set candidates with the policy's recommendation", schemaRef("CDDRuleSetCandidates"),
		"401", "404", "500", "503")}
}

func pathCustomerScoreOverrides() map[string]any {
	return map[string]any{"get": documentedJSONOperation(
		"List proposed overrides of a customer's computed tier",
		[]map[string]any{pathIDParameter("id", "Customer identifier")}, nil,
		"200", "Score overrides, newest first", arraySchema(schemaRef("CDDScoreOverride")),
		"401", "404", "500", "503")}
}

func pathCustomerScoreOverrideApprove() map[string]any {
	params := []map[string]any{
		pathIDParameter("id", "Customer identifier"),
		pathIDParameter("overrideID", "Score override identifier"),
	}
	return map[string]any{"post": documentedJSONOperation(
		"Approve or reject a proposed tier override",
		params, schemaRef("CDDScoreOverrideDecision"),
		"200", "The decided override; on approval the customer's tier moves to the proposed one",
		schemaRef("CDDScoreOverride"), "400", "401", "403", "404", "409", "500", "503")}
}

func pathCustomerEDDAction() map[string]any {
	params := []map[string]any{
		pathIDParameter("id", "Customer identifier"),
		{"name": "action", "in": "path", "required": true, "schema": map[string]any{"type": "string", "enum": []string{"complete", "reopen"}}},
	}
	return map[string]any{"post": documentedJSONOperation(
		"Complete or reopen a customer's EDD window",
		params, schemaRef("EDDActionRequest"),
		"200", "The EDD panel after the action", schemaRef("EDDPanel"),
		"400", "401", "404", "409", "500", "503")}
}

func pathCustomerEDDEvents() map[string]any {
	return map[string]any{"get": documentedJSONOperation(
		"List a customer's EDD lifecycle events",
		[]map[string]any{pathIDParameter("id", "Customer identifier")}, nil,
		"200", "EDD events, newest first", arraySchema(schemaRef("EDDEvent")),
		"401", "404", "500", "503")}
}

func pathPendingEvaluationExport() map[string]any {
	params := []map[string]any{
		{"name": "format", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"csv", "json"}, "default": "csv"}},
		{"name": "status", "in": "query", "description": "Comma-separated queue statuses", "schema": map[string]any{"type": "string"}},
		{"name": "customer_id", "in": "query", "schema": map[string]any{"type": "string"}},
		{"name": "batch_run_id", "in": "query", "schema": map[string]any{"type": "string"}},
		{"name": "created_from", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
		{"name": "created_to", "in": "query", "schema": map[string]any{"type": "string", "format": "date-time"}},
		{"name": "min_age_days", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 0}},
		{"name": "max_age_days", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 0}},
	}
	return map[string]any{"get": map[string]any{
		"summary":     "Export the monitoring gap queue as evidence",
		"description": "Returns every record matching the same filter the listing endpoint accepts. Requires audit:read.",
		"parameters":  params,
		"responses": successWithErrors("200", "Pending evaluation export",
			map[string]any{"oneOf": []any{arraySchema(schemaRef("PendingEvaluation")), map[string]any{"type": "string", "format": "binary"}}},
			"400", "401", "403", "500", "503"),
	}}
}

func pathBacktestAffectedCustomers() map[string]any {
	params := append([]map[string]any{pathIDParameter("id", "Backtest job identifier")}, paginationParams()...)
	params = append(params, map[string]any{"name": "scenario_id", "in": "query", "schema": map[string]any{"type": "string"}})
	return map[string]any{"get": documentedJSONOperation("List affected customers for a backtest comparison", params, nil, "200", "Affected customer IDs", schemaRef("PaginatedAffectedBacktestCustomers"), "400", "401", "404", "429", "500", "503")}
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
	postParams := []map[string]any{{
		"name": "Idempotency-Key", "in": "header", "required": false,
		"description": "Optional key used to make a retried transaction create exactly once",
		"schema":      map[string]any{"type": "string"},
	}}
	return map[string]any{
		"get": map[string]any{
			"summary":    "List Transactions",
			"parameters": params,
			"responses":  successWithErrors("200", "Transactions", schemaRef("PaginatedTransactions"), "400", "401", "404", "429", "500", "503"),
		},
		"post": documentedJSONOperation("Create Transaction", postParams, schemaRef("CreateTransactionRequest"), "201", "Created transaction", schemaRef("Transaction"), "400", "401", "409", "422", "500", "503"),
	}
}

func pathTransactionGet() map[string]any {
	return map[string]any{"get": documentedJSONOperation("Get transaction", []map[string]any{pathIDParameter("id", "Transaction identifier")}, nil, "200", "Transaction", schemaRef("Transaction"), "401", "404", "500", "503")}
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
