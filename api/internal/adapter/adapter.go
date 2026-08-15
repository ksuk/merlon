package adapter

import (
	"context"
	"time"
)

type CustomerData struct {
	ExternalID   string
	Name         string
	Country      string
	CustomerType string
	RawFields    map[string]any
}

type CustomerPage struct {
	Customers  []CustomerData
	NextCursor string
	Watermark  string
}

type TransactionData struct {
	ExternalID         string
	Amount             string
	Currency           string
	Type               string
	RawFields          map[string]any
	CustomerExternalID string
	AccountExternalID  string
	Direction          string
	ExecutedAt         time.Time
}

type TransactionPage struct {
	Transactions []TransactionData
	NextCursor   string
	Watermark    string
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

type CustomerPageFetcher interface {
	FetchCustomersPage(ctx context.Context, params map[string]string) (*CustomerPage, error)
}

type TransactionFetcher interface {
	FetchTransactions(ctx context.Context, params map[string]string) ([]TransactionData, error)
}

type TransactionPageFetcher interface {
	FetchTransactionsPage(ctx context.Context, params map[string]string) (*TransactionPage, error)
}

type Adapter interface {
	CustomerFetcher
	TransactionFetcher
	DryRun(ctx context.Context) (*DryRunResult, error)
}
