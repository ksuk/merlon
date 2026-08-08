package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryAlertRepoLifecycleMetadata(t *testing.T) {
	repo := NewMemoryAlertRepo()
	now := time.Now()
	alert := &domain.Alert{ID: "alert-lifecycle", Status: domain.AlertStatusOpen, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), alert.ID, domain.AlertStatusInvestigating, "operator"); err != nil {
		t.Fatalf("open -> investigating: %v", err)
	}
	got, err := repo.Get(context.Background(), alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResolvedAt != nil || got.ResolvedBy != "" {
		t.Fatalf("active alert retained resolution metadata: %+v", got)
	}
	if err := repo.UpdateStatus(context.Background(), alert.ID, domain.AlertStatusClosedFalsePositive, "operator"); err != nil {
		t.Fatalf("investigating -> terminal: %v", err)
	}
	got, _ = repo.Get(context.Background(), alert.ID)
	if got.ResolvedAt == nil || got.ResolvedBy != "operator" {
		t.Fatalf("terminal metadata = (%v, %q), want timestamp and operator", got.ResolvedAt, got.ResolvedBy)
	}
	if err := repo.UpdateStatus(context.Background(), alert.ID, domain.AlertStatusOpen, ""); err == nil {
		t.Fatal("terminal alert was allowed to reopen")
	}
}

func TestMemoryAlertRepoRejectsDirectTerminalClose(t *testing.T) {
	repo := NewMemoryAlertRepo()
	now := time.Now()
	alert := &domain.Alert{ID: "alert-direct-close", Status: domain.AlertStatusOpen, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	err := repo.UpdateStatus(context.Background(), alert.ID, domain.AlertStatusClosedFalsePositive, "operator")
	var invalid *domain.ErrInvalidStateTransition
	if !errors.As(err, &invalid) {
		t.Fatalf("direct terminal close error = %v, want ErrInvalidStateTransition", err)
	}
}

func TestMemoryCaseAlertLifecycleIsAtomicOnValidationFailure(t *testing.T) {
	cases := NewMemoryCaseRepo()
	alerts := NewMemoryAlertRepo()
	lifecycle := NewMemoryCaseAlertLifecycleRepo(cases, alerts)
	now := time.Now()
	caseRecord := &domain.Case{ID: "case-atomic", Status: domain.CaseStatusNew, Priority: domain.CasePriorityMedium, Summary: "atomic", CreatedAt: now, UpdatedAt: now}
	first := &domain.Alert{ID: "alert-atomic-1", Status: domain.AlertStatusOpen, CreatedAt: now, UpdatedAt: now}
	second := &domain.Alert{ID: "alert-atomic-2", Status: domain.AlertStatusInvestigating, CreatedAt: now, UpdatedAt: now}
	for _, a := range []*domain.Alert{first, second} {
		if err := alerts.Create(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	caseRecord.AlertIDs = []string{first.ID, second.ID}
	if err := cases.Create(context.Background(), caseRecord); err != nil {
		t.Fatal(err)
	}

	updatedCase := *caseRecord
	updatedCase.Status = domain.CaseStatusEscalated
	err := lifecycle.UpdateCaseAndAlerts(context.Background(), &updatedCase, caseRecord.UpdatedAt, []domain.AlertStatusTransition{
		{ID: first.ID, From: first.Status, To: domain.AlertStatusEscalated, ExpectedUpdatedAt: first.UpdatedAt},
		// This edge is invalid, so neither the case nor the first alert may move.
		{ID: second.ID, From: second.Status, To: domain.AlertStatusOpen, ExpectedUpdatedAt: second.UpdatedAt},
	})
	var invalid *domain.ErrInvalidStateTransition
	if !errors.As(err, &invalid) {
		t.Fatalf("atomic invalid update error = %v, want ErrInvalidStateTransition", err)
	}

	gotCase, _ := cases.Get(context.Background(), caseRecord.ID)
	gotFirst, _ := alerts.Get(context.Background(), first.ID)
	if gotCase.Status != domain.CaseStatusNew || gotFirst.Status != domain.AlertStatusOpen {
		t.Fatalf("partial atomic update occurred: case=%q first alert=%q", gotCase.Status, gotFirst.Status)
	}
}

func TestMemoryCaseAlertLifecycleRechecksValidationOnlyAlerts(t *testing.T) {
	cases := NewMemoryCaseRepo()
	alerts := NewMemoryAlertRepo()
	lifecycle := NewMemoryCaseAlertLifecycleRepo(cases, alerts)
	now := time.Now()
	caseRecord := &domain.Case{ID: "case-validation-only", Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityMedium, Summary: "validation-only", CreatedAt: now, UpdatedAt: now}
	alert := &domain.Alert{ID: "alert-validation-only", Status: domain.AlertStatusEscalated, CreatedAt: now, UpdatedAt: now}
	if err := cases.Create(context.Background(), caseRecord); err != nil {
		t.Fatal(err)
	}
	if err := alerts.Create(context.Background(), alert); err != nil {
		t.Fatal(err)
	}

	updatedCase := *caseRecord
	updatedCase.Status = domain.CaseStatusEscalated
	alertBefore, err := alerts.Get(context.Background(), alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.UpdateCaseAndAlerts(context.Background(), &updatedCase, caseRecord.UpdatedAt, []domain.AlertStatusTransition{
		{ID: alert.ID, From: alert.Status, To: alert.Status, ExpectedUpdatedAt: alertBefore.UpdatedAt},
	}); err != nil {
		t.Fatalf("validation-only lifecycle update: %v", err)
	}

	gotAlert, err := alerts.Get(context.Background(), alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotAlert.Status != domain.AlertStatusEscalated || !gotAlert.UpdatedAt.Equal(alertBefore.UpdatedAt) {
		t.Fatalf("validation-only alert changed: before=%+v after=%+v", alertBefore, gotAlert)
	}
}
