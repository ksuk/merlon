package domain

import (
	"encoding/json"
	"testing"
)

func TestBacktestResultJSONNormalizesEmptyScenarioResults(t *testing.T) {
	data, err := json.Marshal(BacktestResult{BacktestID: "backtest-1"})
	if err != nil {
		t.Fatalf("marshal backtest result: %v", err)
	}

	var got struct {
		ScenarioResults []BacktestScenarioResult `json:"scenario_results"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal backtest result: %v", err)
	}
	if got.ScenarioResults == nil {
		t.Fatalf("scenario_results = null in %s, want []", data)
	}
}

func TestCompletedBacktestJobJSONNormalizesEmptyScenarioResults(t *testing.T) {
	data, err := json.Marshal(BacktestJob{
		ID:        "job-1",
		Status:    BacktestJobCompleted,
		Candidate: &BacktestResult{BacktestID: "backtest-1"},
	})
	if err != nil {
		t.Fatalf("marshal backtest job: %v", err)
	}

	var got struct {
		Candidate *struct {
			ScenarioResults []BacktestScenarioResult `json:"scenario_results"`
		} `json:"candidate"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal backtest job: %v", err)
	}
	if got.Candidate == nil || got.Candidate.ScenarioResults == nil {
		t.Fatalf("candidate scenario_results = null in %s, want []", data)
	}
}
