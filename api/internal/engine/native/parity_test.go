package native

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

type parityRecord struct {
	Operation string          `json:"operation"`
	CaseID    string          `json:"case_id"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
}

type parityTransaction struct {
	ID                  string  `json:"id"`
	CustomerID          string  `json:"customer_id"`
	Amount              float64 `json:"amount"`
	Currency            string  `json:"currency"`
	Direction           string  `json:"direction"`
	ExecutedAtSecs      int64   `json:"executed_at_secs"`
	CounterpartyCountry string  `json:"counterparty_country"`
}

func TestGoldenCorpusParity(t *testing.T) {
	parityDir := filepath.Join("testdata", "parity")
	engine := newParityFixtureEngine(t, parityDir)
	corpus, err := os.Open(filepath.Join(parityDir, "corpus.jsonl"))
	if err != nil {
		t.Fatalf("open frozen parity corpus: %v", err)
	}
	defer corpus.Close()

	scanner := bufio.NewScanner(corpus)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	count := 0
	for scanner.Scan() {
		var record parityRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode corpus line %d: %v", count+1, err)
		}
		actual, err := replayParityRecord(context.Background(), engine, record)
		if err != nil {
			t.Fatalf("replay %s/%s: %v", record.Operation, record.CaseID, err)
		}
		assertParityJSON(t, record.Operation+"/"+record.CaseID, record.Output, actual)
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("parity corpus is empty")
	}
}

func newParityFixtureEngine(t *testing.T, parityDir string) *Engine {
	t.Helper()
	tmp := t.TempDir()
	cdd := copyParityFile(t, parityDir, "cdd.yaml", tmp)
	country := copyParityFile(t, parityDir, "country.yaml", tmp)
	screening := copyParityFile(t, parityDir, "screening.yaml", tmp)
	tmDir := filepath.Join(tmp, "tm")
	if err := os.Mkdir(tmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tm_structuring.yaml", "tm_rapid.yaml", "tm_hfsa.yaml", "tm_dormant.yaml", "tm_highrisk.yaml"} {
		copyParityFile(t, parityDir, name, tmDir)
	}
	e, err := New(cdd, tmDir, screening, country)
	if err != nil {
		t.Fatalf("load parity engine: %v", err)
	}
	return e
}

func copyParityFile(t *testing.T, dir, name, destination string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(destination, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func replayParityRecord(ctx context.Context, e *Engine, record parityRecord) ([]byte, error) {
	switch record.Operation {
	case "scoring":
		var input struct {
			CustomerID   string              `json:"customer_id"`
			CustomerType domain.CustomerType `json:"customer_type"`
			CountryCode  string              `json:"country_code"`
			ProductTypes []string            `json:"product_types"`
			Attributes   map[string]any      `json:"attributes"`
			RuleSetID    string              `json:"rule_set_id"`
		}
		if err := json.Unmarshal(record.Input, &input); err != nil {
			return nil, err
		}
		score, err := e.ScoreCustomer(ctx, &domain.Customer{ID: input.CustomerID, CustomerType: input.CustomerType, CountryCode: input.CountryCode, ProductTypes: input.ProductTypes, Attributes: input.Attributes}, input.RuleSetID)
		if err != nil {
			return nil, err
		}
		factors := make([]map[string]any, 0, len(score.Factors))
		for _, factor := range score.Factors {
			factors = append(factors, map[string]any{"name": factor.Name, "axis": factor.Axis, "score": factor.Score, "description": factor.Description})
		}
		return json.Marshal(map[string]any{"customer_id": score.CustomerID, "score": score.Score, "tier": strings.ToUpper(string(score.Tier)), "factors": factors, "rule_set_id": score.RuleSetID, "rule_set_sha256": score.RuleSetSHA256, "rule_set_version": score.RuleSetVersion})
	case "monitoring":
		var input struct {
			CustomerID   string                `json:"customer_id"`
			CustomerType domain.CustomerType   `json:"customer_type"`
			RiskTier     domain.RiskTier       `json:"risk_tier"`
			Mode         engine.EvaluationMode `json:"mode"`
			ScenarioIDs  []string              `json:"scenario_ids"`
			Transactions []parityTransaction   `json:"transactions"`
		}
		if err := json.Unmarshal(record.Input, &input); err != nil {
			return nil, err
		}
		transactions := make([]domain.Transaction, 0, len(input.Transactions))
		for _, tx := range input.Transactions {
			transactions = append(transactions, parityDomainTransaction(tx))
		}
		alerts, err := e.Evaluate(ctx, engine.MonitoringRequest{CustomerID: input.CustomerID, CustomerType: input.CustomerType, RiskTier: input.RiskTier, Transactions: transactions, ScenarioIDs: input.ScenarioIDs, Mode: input.Mode})
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(alerts))
		for _, alert := range alerts {
			out = append(out, map[string]any{"scenario_id": alert.ScenarioID, "severity": strings.ToUpper(string(alert.Severity)), "customer_id": alert.CustomerID, "transaction_ids": alert.TransactionIDs, "description": alert.Description, "score": alert.Score})
		}
		return json.Marshal(map[string]any{"alerts": out})
	case "screening":
		var input struct {
			CustomerID string   `json:"customer_id"`
			Name       string   `json:"name"`
			ListIDs    []string `json:"list_ids"`
		}
		if err := json.Unmarshal(record.Input, &input); err != nil {
			return nil, err
		}
		result, err := e.ScreenCustomer(ctx, &domain.Customer{ID: input.CustomerID, ExternalID: input.Name, Attributes: map[string]any{"name": input.Name}}, input.ListIDs)
		if err != nil {
			return nil, err
		}
		matches := make([]map[string]any, 0, len(result.Matches))
		for _, match := range result.Matches {
			matches = append(matches, map[string]any{"list_id": match.ListID, "entry_id": match.EntryID, "matched_name": match.MatchedName, "similarity": match.Similarity, "list_type": match.ListType, "source": match.Source})
		}
		return json.Marshal(map[string]any{"customer_id": result.CustomerID, "hit": result.Hit, "matches": matches, "lists_checked": result.ListsChecked})
	case "backtest":
		var input struct {
			Customers    []struct{ ID, CustomerType, RiskTier string } `json:"customers"`
			Transactions []parityTransaction                           `json:"transactions"`
			ScenarioIDs  []string                                      `json:"scenario_ids"`
			Description  string                                        `json:"description"`
		}
		if err := json.Unmarshal(record.Input, &input); err != nil {
			return nil, err
		}
		customers := make([]domain.Customer, 0, len(input.Customers))
		for _, c := range input.Customers {
			var tier *domain.RiskTier
			if c.RiskTier != "" {
				t := domain.RiskTier(strings.ToLower(c.RiskTier))
				tier = &t
			}
			customers = append(customers, domain.Customer{ID: c.ID, CustomerType: domain.CustomerType(c.CustomerType), RiskTier: tier})
		}
		transactions := make([]domain.Transaction, 0, len(input.Transactions))
		for _, tx := range input.Transactions {
			transactions = append(transactions, parityDomainTransaction(tx))
		}
		result, err := e.RunBacktest(ctx, customers, transactions, input.ScenarioIDs, input.Description)
		if err != nil {
			return nil, err
		}
		scenarios := make([]map[string]any, 0, len(result.ScenarioResults))
		for _, scenario := range result.ScenarioResults {
			scenarios = append(scenarios, map[string]any{"scenario_id": scenario.ScenarioID, "alerts_generated": scenario.AlertsGenerated, "high_severity_count": scenario.HighSeverityCount, "medium_severity_count": scenario.MediumSeverityCount, "low_severity_count": scenario.LowSeverityCount, "affected_customer_ids": scenario.AffectedCustomerIDs})
		}
		return json.Marshal(map[string]any{"total_transactions": result.TotalTransactions, "total_customers": result.TotalCustomers, "total_alerts": result.TotalAlerts, "scenario_results": scenarios})
	case "config":
		var input struct {
			ConfigType  string `json:"config_type"`
			YAMLContent string `json:"yaml_content"`
		}
		if err := json.Unmarshal(record.Input, &input); err != nil {
			return nil, err
		}
		result, err := e.ValidateConfig(ctx, input.ConfigType, input.YAMLContent)
		if err != nil {
			return nil, err
		}
		fields := make([]string, 0, len(result.Errors))
		for _, validationErr := range result.Errors {
			fields = append(fields, validationErr.Field)
		}
		return json.Marshal(map[string]any{"valid": result.Valid, "error_fields": fields})
	default:
		return nil, fmt.Errorf("unknown operation %q", record.Operation)
	}
}

func parityDomainTransaction(tx parityTransaction) domain.Transaction {
	return domain.Transaction{ID: tx.ID, CustomerID: tx.CustomerID, Amount: tx.Amount, Currency: tx.Currency, Direction: domain.TransactionDirection(tx.Direction), ExecutedAt: time.Unix(tx.ExecutedAtSecs, 0).UTC(), CounterpartyCountry: tx.CounterpartyCountry}
}

func assertParityJSON(t *testing.T, name string, expected, actual []byte) {
	t.Helper()
	var expectedValue, actualValue any
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		t.Fatalf("%s expected JSON: %v", name, err)
	}
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatalf("%s actual JSON: %v", name, err)
	}
	if !reflect.DeepEqual(expectedValue, actualValue) {
		t.Errorf("%s mismatch\nexpected: %s\nactual:   %s", name, expected, actual)
	}
}
