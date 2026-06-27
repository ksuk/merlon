package adapter

import "context"

type CustomerData struct {
	ExternalID   string
	Name         string
	Country      string
	CustomerType string
	RawFields    map[string]any
}

type TransactionData struct {
	ExternalID string
	Amount     string
	Currency   string
	Type       string
	RawFields  map[string]any
}

type DryRunResult struct {
	ConfigValid     bool              `json:"config_valid"`
	Reachable       bool              `json:"reachable"`
	AuthValid       bool              `json:"auth_valid"`
	EndpointResults map[string]string `json:"endpoint_results"`
	Errors          []string          `json:"errors"`
}

type CustomerFetcher interface {
	FetchCustomer(ctx context.Context, id string) (*CustomerData, error)
}

type TransactionFetcher interface {
	FetchTransactions(ctx context.Context, params map[string]string) ([]TransactionData, error)
}

type Adapter interface {
	CustomerFetcher
	TransactionFetcher
	DryRun(ctx context.Context) (*DryRunResult, error)
}
