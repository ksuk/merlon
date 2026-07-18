package engine

import (
	"context"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

type recordingMonitoring struct {
	realtimeCalls int
	batchCalls    int
}

func (m *recordingMonitoring) EvaluateTransactions(context.Context, string, domain.RiskTier, []domain.Transaction, []string) ([]domain.Alert, error) {
	m.realtimeCalls++
	return nil, nil
}

func (m *recordingMonitoring) EvaluateTransactionsBatch(context.Context, string, domain.RiskTier, []domain.Transaction, []string) ([]domain.Alert, error) {
	m.batchCalls++
	return nil, nil
}

type recordingMonitoringV2 struct {
	recordingMonitoring
	request MonitoringRequest
}

func (m *recordingMonitoringV2) Evaluate(_ context.Context, req MonitoringRequest) ([]domain.Alert, error) {
	m.request = req
	return nil, nil
}

func TestEvaluateCompatPrefersV2(t *testing.T) {
	m := &recordingMonitoringV2{}
	req := MonitoringRequest{CustomerID: "c1", CustomerType: domain.CustomerTypeIndividual, Mode: EvaluationModeBatch}
	if _, err := EvaluateCompat(context.Background(), m, req); err != nil {
		t.Fatal(err)
	}
	if m.request.CustomerID != req.CustomerID || m.request.CustomerType != req.CustomerType || m.request.Mode != req.Mode {
		t.Fatalf("request = %+v, want %+v", m.request, req)
	}
	if m.realtimeCalls != 0 || m.batchCalls != 0 {
		t.Fatalf("legacy calls: realtime=%d batch=%d", m.realtimeCalls, m.batchCalls)
	}
}

func TestEvaluateCompatUsesModeForLegacyEngine(t *testing.T) {
	tests := []struct {
		name         string
		mode         EvaluationMode
		wantRealtime int
		wantBatch    int
	}{
		{name: "realtime", mode: EvaluationModeRealtime, wantRealtime: 1},
		{name: "batch", mode: EvaluationModeBatch, wantBatch: 1},
		{name: "both uses realtime legacy adapter", mode: EvaluationModeBoth, wantRealtime: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &recordingMonitoring{}
			if _, err := EvaluateCompat(context.Background(), m, MonitoringRequest{CustomerID: "c1", Mode: tt.mode}); err != nil {
				t.Fatal(err)
			}
			if m.realtimeCalls != tt.wantRealtime || m.batchCalls != tt.wantBatch {
				t.Fatalf("calls: realtime=%d batch=%d", m.realtimeCalls, m.batchCalls)
			}
		})
	}
}
