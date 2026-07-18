package native

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func BenchmarkNativeStructuringFiveThousandEvents(b *testing.B) {
	dir := b.TempDir()
	cddPath := filepath.Join(dir, "cdd.yaml")
	tmDir := filepath.Join(dir, "tm")
	if err := os.Mkdir(tmDir, 0o700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(cddPath, []byte("schema_version: cdd_weight_v1\npreset_id: b\nrisk_factors: {x: {weight: 1, values: {v: 1}}}\ntier_thresholds: {LOW: {max: 2}, MEDIUM: {min: 2, max: 3}, HIGH: {min: 3}}\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmDir, "s.yaml"), []byte("scenario_id: tm_structuring_basic\nconditions: {threshold: {by_customer_type: {individual: {by_risk_tier: {LOW: 1000000000}}}}, additional: {min_transaction_count: 3, individual_below: 1000000}}\nevaluation_mode: batch\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	e, err := New(cddPath, tmDir, filepath.Join(dir, "none"), "")
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now()
	txns := make([]domain.Transaction, 5000)
	for i := range txns {
		txns[i] = domain.Transaction{ID: strconv.Itoa(i), CustomerID: "c1", Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now.Add(time.Duration(i) * time.Second)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.EvaluateTransactionsBatch(context.Background(), "c1", domain.RiskTierLow, txns, nil)
	}
}
