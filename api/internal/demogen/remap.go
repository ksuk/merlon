package demogen

// remapIDsToUUIDs rewrites every demogen entity's primary-key ID and
// cross-reference from its human-readable generation-time label (e.g.
// "demo-story-01", "demo-txn-0000001") to a deterministic UUID (uuidFor),
// as the very last step of Generate — every self-check, story-wiring, and
// STORY_IDS.md-authoring step upstream of this call runs on the plain
// labels, which stay far more readable in error messages and in the
// generator's own source (see cases.go, story.go, storyids.go).
//
// uuidFor(label) is a pure function, so remapping a cross-reference is
// just uuidFor of the same label used for the referenced entity's own ID —
// no lookup table is needed, and the result does not depend on the order
// these loops run in.
//
// Deliberately NOT remapped:
//   - Customer.ExternalID (MNP-*): a human-readable business key, not an
//     internal ID.
//   - ScreeningResultRecord.EntryID (e.g. "DEMO-SANCTIONS-001"): an
//     external screening-list entry reference, not a demogen entity.
//   - RuleDefinition.Name: RuleRepository's actual lookup key (see
//     store.PgRuleRepo's doc comment — Get/GetActive/SetActive all take
//     the rule's Name, not its ID row); remapping it would break rule
//     lookups for no benefit, and rule_definitions.name is a TEXT column
//     with no format constraint.
//   - CaseNote.Author / AuditEntry.UserID: analyst usernames.
//   - AuditEntry.ID: a bigserial sequence number the store always
//     reassigns on insert regardless of what's supplied (see
//     api/internal/seed/loader.go and store.PgAuditRepo.Create /
//     store.MemoryAuditRepo.Create), so giving it a UUID would be
//     meaningless.
func remapIDsToUUIDs(r *Result) {
	for i := range r.Customers {
		r.Customers[i].ID = uuidFor(r.Customers[i].ID)
	}

	for i := range r.Accounts {
		r.Accounts[i].ID = uuidFor(r.Accounts[i].ID)
	}
	for i := range r.AccountCustomers {
		r.AccountCustomers[i].AccountID = uuidFor(r.AccountCustomers[i].AccountID)
		r.AccountCustomers[i].CustomerID = uuidFor(r.AccountCustomers[i].CustomerID)
	}

	for i := range r.ScoreHistory {
		r.ScoreHistory[i].ID = uuidFor(r.ScoreHistory[i].ID)
		r.ScoreHistory[i].CustomerID = uuidFor(r.ScoreHistory[i].CustomerID)
	}

	for i := range r.Transactions {
		r.Transactions[i].ID = uuidFor(r.Transactions[i].ID)
		r.Transactions[i].CustomerID = uuidFor(r.Transactions[i].CustomerID)
	}

	for i := range r.Alerts {
		r.Alerts[i].ID = uuidFor(r.Alerts[i].ID)
		r.Alerts[i].CustomerID = uuidFor(r.Alerts[i].CustomerID)
		for j := range r.Alerts[i].TransactionIDs {
			r.Alerts[i].TransactionIDs[j] = uuidFor(r.Alerts[i].TransactionIDs[j])
		}
	}

	for i := range r.Cases {
		r.Cases[i].ID = uuidFor(r.Cases[i].ID)
		r.Cases[i].CustomerID = uuidFor(r.Cases[i].CustomerID)
		for j := range r.Cases[i].AlertIDs {
			r.Cases[i].AlertIDs[j] = uuidFor(r.Cases[i].AlertIDs[j])
		}
	}
	for i := range r.CaseNotes {
		r.CaseNotes[i].ID = uuidFor(r.CaseNotes[i].ID)
		r.CaseNotes[i].CaseID = uuidFor(r.CaseNotes[i].CaseID)
	}

	for i := range r.ScreeningResults {
		r.ScreeningResults[i].ID = uuidFor(r.ScreeningResults[i].ID)
		r.ScreeningResults[i].CustomerID = uuidFor(r.ScreeningResults[i].CustomerID)
	}

	for i := range r.RuleDefinitions {
		r.RuleDefinitions[i].ID = uuidFor(r.RuleDefinitions[i].ID)
	}

	// AuditEntry.ResourceID is not FK-constrained (audit_logs.resource_id is
	// plain VARCHAR), but buildAuditLogs only ever emits resource_type in
	// {customers, cases, screening_results, rule_definitions} — every one of
	// those is remapped above — so the audit trail must point at the same
	// UUID the referenced entity now actually has (Auditability First).
	for i := range r.AuditLogs {
		r.AuditLogs[i].ResourceID = uuidFor(r.AuditLogs[i].ResourceID)
	}

	for i := range r.StoryCustomerIDs {
		r.StoryCustomerIDs[i] = uuidFor(r.StoryCustomerIDs[i])
	}
}
