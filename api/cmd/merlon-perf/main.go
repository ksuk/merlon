// Command merlon-perf produces reproducible localhost-only performance
// evidence against a running Merlon release candidate.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/buildinfo"
)

const (
	syntheticCustomerName = "Synthetic Performance Customer"
	performanceTokenEnv   = "MERLON_PERF_BEARER_TOKEN"
	dataSourceDescription = "built-in synthetic performance fixtures"
	targetStateReady      = "ready"
	targetStateUnknown    = "unknown"
	targetReasonNoProbe   = "no_probe_available"
)

var exactCommitPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type options struct {
	baseURL        string
	expectedCommit string
	requests       int
	concurrency    int
	customers      int
	warmup         int
	requestTimeout time.Duration
	runID          string
}

type targetStatus struct {
	Version      string                  `json:"version"`
	Commit       string                  `json:"commit"`
	BuiltAt      string                  `json:"built_at"`
	AuthMode     string                  `json:"auth_mode"`
	BaseCurrency string                  `json:"base_currency"`
	Components   []targetComponentStatus `json:"components"`
}

type targetComponentStatus struct {
	Name             string `json:"name"`
	Configured       bool   `json:"configured"`
	OperationalState string `json:"operational_state"`
	ReasonCode       string `json:"reason_code,omitempty"`
}

type syntheticCustomerRequest struct {
	ExternalID   string         `json:"external_id"`
	CustomerType string         `json:"customer_type"`
	CountryCode  string         `json:"country_code"`
	ProductTypes []string       `json:"product_types"`
	Attributes   map[string]any `json:"attributes"`
}

type syntheticTransactionRequest struct {
	CustomerID string    `json:"customer_id"`
	ExternalID string    `json:"external_id"`
	Amount     float64   `json:"amount"`
	Currency   string    `json:"currency"`
	Direction  string    `json:"direction"`
	Channel    string    `json:"channel"`
	ExecutedAt time.Time `json:"executed_at"`
	Metadata   any       `json:"metadata,omitempty"`
}

type targetReport struct {
	BaseURL      string                  `json:"base_url"`
	Version      string                  `json:"version"`
	Commit       string                  `json:"commit"`
	BuiltAt      string                  `json:"built_at,omitempty"`
	AuthMode     string                  `json:"auth_mode"`
	BaseCurrency string                  `json:"base_currency"`
	Components   []targetComponentStatus `json:"components"`
}

type harnessReport struct {
	Version    string `json:"version"`
	Commit     string `json:"commit,omitempty"`
	BuiltAt    string `json:"built_at,omitempty"`
	GoVersion  string `json:"go_version"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	NumCPU     int    `json:"num_cpu"`
	GOMAXPROCS int    `json:"gomaxprocs"`
}

type measurementReport struct {
	Endpoint         string `json:"endpoint"`
	DataSource       string `json:"data_source"`
	CustomerCount    int    `json:"customer_count"`
	WarmupRequests   int    `json:"warmup_requests"`
	MeasuredRequests int    `json:"measured_requests"`
	Concurrency      int    `json:"concurrency"`
	RequestTimeoutMS int64  `json:"request_timeout_ms"`
}

type performanceReport struct {
	SchemaVersion int               `json:"schema_version"`
	Target        targetReport      `json:"target"`
	Harness       harnessReport     `json:"harness"`
	Measurement   measurementReport `json:"measurement"`
	StartedAt     time.Time         `json:"started_at"`
	CompletedAt   time.Time         `json:"completed_at"`
	DurationMS    float64           `json:"duration_ms"`
	Results       resultSummary     `json:"results"`
}

func main() {
	if err := runCLI(context.Background(), os.Args[1:], os.Getenv, os.Stdout); err != nil {
		slog.Error("performance evidence failed", "error", err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if err := validateHarnessCommit(buildinfo.Commit, opts.expectedCommit); err != nil {
		return err
	}
	report, runErr := execute(ctx, opts, getenv(performanceTokenEnv))
	if report.SchemaVersion != 0 {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	return runErr
}

func validateHarnessCommit(harnessCommit, expectedCommit string) error {
	normalized := strings.ToLower(harnessCommit)
	if !exactCommitPattern.MatchString(normalized) {
		return errors.New("harness commit must be an exact 40-character hexadecimal SHA")
	}
	if normalized != strings.ToLower(expectedCommit) {
		return fmt.Errorf("harness commit %s does not match expected commit %s", normalized, expectedCommit)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	var out options
	fs := flag.NewFlagSet("merlon-perf", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&out.baseURL, "base-url", "", "loopback Merlon base URL")
	fs.StringVar(&out.expectedCommit, "expected-commit", "", "exact 40-character target commit SHA")
	fs.IntVar(&out.requests, "requests", 1000, "number of measured transaction requests")
	fs.IntVar(&out.concurrency, "concurrency", 16, "number of concurrent workers")
	fs.IntVar(&out.customers, "customers", 0, "number of synthetic customers (default: concurrency)")
	fs.IntVar(&out.warmup, "warmup", 100, "number of unmeasured warmup requests")
	fs.DurationVar(&out.requestTimeout, "request-timeout", 15*time.Second, "timeout for each HTTP request")
	if err := fs.Parse(args); err != nil {
		return out, err
	}
	if fs.NArg() != 0 {
		return out, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if _, err := validateLoopbackBaseURL(out.baseURL); err != nil {
		return out, fmt.Errorf("--base-url: %w", err)
	}
	if !exactCommitPattern.MatchString(out.expectedCommit) {
		return out, errors.New("--expected-commit must be an exact 40-character hexadecimal commit SHA")
	}
	out.expectedCommit = strings.ToLower(out.expectedCommit)
	if out.requests <= 0 {
		return out, errors.New("--requests must be positive")
	}
	if out.concurrency <= 0 {
		return out, errors.New("--concurrency must be positive")
	}
	if out.customers == 0 {
		out.customers = out.concurrency
	}
	if out.customers < 0 {
		return out, errors.New("--customers must be positive")
	}
	if out.warmup < 0 {
		return out, errors.New("--warmup must not be negative")
	}
	if out.requestTimeout <= 0 {
		return out, errors.New("--request-timeout must be positive")
	}
	return out, nil
}

func execute(ctx context.Context, opts options, bearerToken string) (performanceReport, error) {
	baseURL, err := validateLoopbackBaseURL(opts.baseURL)
	if err != nil {
		return performanceReport{}, err
	}
	if !exactCommitPattern.MatchString(opts.expectedCommit) {
		return performanceReport{}, errors.New("expected commit must be an exact 40-character hexadecimal SHA")
	}
	if opts.requests <= 0 || opts.concurrency <= 0 || opts.customers <= 0 || opts.warmup < 0 || opts.requestTimeout <= 0 {
		return performanceReport{}, errors.New("request, concurrency, customer, warmup, or timeout option is invalid")
	}
	client := newLoopbackHTTPClient(opts.requestTimeout)
	status, err := fetchTargetStatus(ctx, client, baseURL, bearerToken, strings.ToLower(opts.expectedCommit))
	if err != nil {
		return performanceReport{}, err
	}
	if opts.runID == "" {
		opts.runID, err = newRunID()
		if err != nil {
			return performanceReport{}, err
		}
	}

	customerIDs, err := createSyntheticCustomers(ctx, client, baseURL, bearerToken, opts.runID, opts.customers)
	if err != nil {
		return performanceReport{}, err
	}
	if opts.warmup > 0 {
		warmupStarted := time.Now().UTC()
		warmupResults := runTransactions(ctx, client, baseURL, bearerToken, opts.runID, "W", opts.warmup, opts.concurrency, customerIDs)
		warmupSummary := summarizeResults(warmupStarted, time.Now().UTC(), warmupResults)
		if warmupSummary.Failed != 0 {
			return performanceReport{}, fmt.Errorf("warmup failed: %d of %d requests", warmupSummary.Failed, warmupSummary.Attempted)
		}
	}

	started := time.Now().UTC()
	results := runTransactions(ctx, client, baseURL, bearerToken, opts.runID, "T", opts.requests, opts.concurrency, customerIDs)
	completed := time.Now().UTC()
	summary := summarizeResults(started, completed, results)
	report := performanceReport{
		SchemaVersion: 1,
		Target: targetReport{
			BaseURL: baseURL.String(), Version: status.Version, Commit: status.Commit,
			BuiltAt: status.BuiltAt, AuthMode: status.AuthMode, BaseCurrency: status.BaseCurrency,
			Components: status.Components,
		},
		Harness: harnessReport{
			Version: buildinfo.Version, Commit: buildinfo.Commit, BuiltAt: buildinfo.BuiltAt,
			GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
			NumCPU: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0),
		},
		Measurement: measurementReport{
			Endpoint: "/api/v1/transactions", DataSource: dataSourceDescription,
			CustomerCount: opts.customers, WarmupRequests: opts.warmup,
			MeasuredRequests: opts.requests, Concurrency: opts.concurrency,
			RequestTimeoutMS: opts.requestTimeout.Milliseconds(),
		},
		StartedAt: started, CompletedAt: completed,
		DurationMS: durationMilliseconds(completed.Sub(started)), Results: summary,
	}
	if summary.Failed != 0 {
		return report, fmt.Errorf("measurement failed: %d of %d requests", summary.Failed, summary.Attempted)
	}
	return report, nil
}

func fetchTargetStatus(ctx context.Context, client *http.Client, baseURL *url.URL, bearerToken, expectedCommit string) (targetStatus, error) {
	target := *baseURL
	target.Path = "/api/v1/system/status"
	target.RawQuery = "refresh=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return targetStatus{}, err
	}
	setBearerToken(req, bearerToken)
	response, err := client.Do(req)
	if err != nil {
		return targetStatus{}, fmt.Errorf("read target status: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return targetStatus{}, fmt.Errorf("read target status: HTTP %d", response.StatusCode)
	}
	var status targetStatus
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&status); err != nil {
		return targetStatus{}, fmt.Errorf("decode target status: %w", err)
	}
	status.Commit = strings.ToLower(status.Commit)
	if status.Commit == "" {
		return targetStatus{}, errors.New("target status does not report a commit")
	}
	if status.Commit != expectedCommit {
		return targetStatus{}, fmt.Errorf("target commit %s does not match expected commit %s", status.Commit, expectedCommit)
	}
	components := make(map[string]targetComponentStatus, len(status.Components))
	for _, component := range status.Components {
		components[component.Name] = component
	}
	for _, required := range []string{"api", "database", "engine"} {
		component, ok := components[required]
		if !ok {
			return targetStatus{}, fmt.Errorf("target status does not report required component %s", required)
		}
		if !component.Configured {
			return targetStatus{}, fmt.Errorf("target component %s is not configured", component.Name)
		}
		if required == "engine" && component.OperationalState == targetStateUnknown && component.ReasonCode == targetReasonNoProbe {
			continue
		}
		if component.OperationalState != targetStateReady {
			return targetStatus{}, fmt.Errorf(
				"target component %s is not ready (configured=%t, operational_state=%s, reason_code=%s)",
				component.Name, component.Configured, component.OperationalState, component.ReasonCode,
			)
		}
	}
	return status, nil
}

func createSyntheticCustomers(ctx context.Context, client *http.Client, baseURL *url.URL, bearerToken, runID string, count int) ([]string, error) {
	ids := make([]string, 0, count)
	for index := 0; index < count; index++ {
		payload := syntheticCustomerRequest{
			ExternalID:   fmt.Sprintf("MERLON-PERF-%s-C%04d", runID, index+1),
			CustomerType: "individual", CountryCode: "JP",
			ProductTypes: []string{"synthetic-performance"},
			Attributes: map[string]any{
				"name": syntheticCustomerName, "date_of_birth": "1990-01-01",
				"address": "Synthetic Performance Address", "occupation": "Synthetic Tester",
				"nationality": "JP",
			},
		}
		response, err := postJSON(ctx, client, baseURL, bearerToken, "/api/v1/customers", payload)
		if err != nil {
			return nil, fmt.Errorf("create synthetic customer %d: %w", index+1, err)
		}
		if response.StatusCode != http.StatusCreated {
			response.Body.Close()
			return nil, fmt.Errorf("create synthetic customer %d: HTTP %d", index+1, response.StatusCode)
		}
		var created struct {
			ID string `json:"id"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&created)
		closeErr := response.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode synthetic customer %d: %w", index+1, decodeErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if created.ID == "" {
			return nil, fmt.Errorf("create synthetic customer %d: response ID is empty", index+1)
		}
		ids = append(ids, created.ID)
	}
	return ids, nil
}

func runTransactions(ctx context.Context, client *http.Client, baseURL *url.URL, bearerToken, runID, phase string, count, concurrency int, customerIDs []string) []requestResult {
	jobs := make(chan int, count)
	results := make(chan requestResult, count)
	for index := 0; index < count; index++ {
		jobs <- index
	}
	close(jobs)

	workers := concurrency
	if workers > count {
		workers = count
	}
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wait.Done()
			for index := range jobs {
				payload := syntheticTransactionRequest{
					CustomerID: customerIDs[index%len(customerIDs)],
					ExternalID: fmt.Sprintf("MERLON-PERF-%s-%s%07d", runID, phase, index+1),
					Amount:     1000 + float64(index%100), Currency: "JPY", Direction: "internal",
					Channel: "synthetic_performance", ExecutedAt: time.Now().UTC(),
				}
				started := time.Now()
				response, err := postJSON(ctx, client, baseURL, bearerToken, "/api/v1/transactions", payload)
				result := requestResult{duration: time.Since(started)}
				if err != nil {
					result.transportError = true
				} else {
					result.statusCode = response.StatusCode
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
					_ = response.Body.Close()
				}
				results <- result
			}
		}()
	}
	wait.Wait()
	close(results)

	collected := make([]requestResult, 0, count)
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

func postJSON(ctx context.Context, client *http.Client, baseURL *url.URL, bearerToken, path string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	target := *baseURL
	target.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearerToken(req, bearerToken)
	return client.Do(req)
}

func setBearerToken(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func newRunID() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate run ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}
