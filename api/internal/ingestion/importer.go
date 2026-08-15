// Package ingestion contains the repository-backed external bulk import
// application service. It accepts only source records and deliberately does
// not accept alerts, cases, screening results, score history, or other
// derived artifacts as input.
package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ksuk/merlon/api/internal/domain"
)

const (
	customersFile        = "customers.csv"
	accountsFile         = "accounts.csv"
	accountCustomersFile = "account_customers.csv"
	transactionsFile     = "transactions.csv"
)

var fixedHeaders = map[string][]string{
	customersFile:        {"external_id", "customer_type", "country_code", "status", "product_types_json", "attributes_json"},
	accountsFile:         {"external_id", "account_type"},
	accountCustomersFile: {"account_external_id", "customer_external_id", "role"},
	transactionsFile:     {"external_id", "customer_external_id", "account_external_id", "amount", "currency", "direction", "transaction_type", "executed_at", "counterparty_id", "counterparty_country", "channel", "metadata_json"},
}

type DuplicateMode string

const (
	DuplicateSkip  DuplicateMode = "skip"
	DuplicateError DuplicateMode = "error"
)

type Options struct {
	SourceDir   string
	DryRun      bool
	Apply       bool
	OnDuplicate DuplicateMode
	Actor       string
	ReportJSON  string
}

type RecordOutcome struct {
	EntityType    string `json:"entity_type"`
	ExternalID    string `json:"external_id"`
	SourceFile    string `json:"source_file"`
	Line          int    `json:"line"`
	PayloadSHA256 string `json:"payload_sha256"`
	Status        string `json:"status"`
	ReasonCode    string `json:"reason_code,omitempty"`
}

type Report struct {
	ID          string            `json:"id"`
	SourceDir   string            `json:"source_dir"`
	Actor       string            `json:"actor,omitempty"`
	DryRun      bool              `json:"dry_run"`
	Applied     bool              `json:"applied"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt time.Time         `json:"completed_at"`
	SourceFiles map[string]string `json:"source_files"`
	Counts      map[string]int    `json:"counts"`
	Outcomes    []RecordOutcome   `json:"outcomes"`
}

type Dependencies struct {
	Customers    domain.CustomerRepository
	Accounts     domain.AccountRepository
	Transactions domain.TransactionRepository
}

type Importer struct {
	Deps Dependencies
}

type parsedBundle struct {
	customers    []customerRow
	accounts     []accountRow
	links        []linkRow
	transactions []transactionRow
	files        map[string]string
}

type customerRow struct {
	externalID   string
	customerType domain.CustomerType
	country      string
	status       domain.CustomerStatus
	productTypes []string
	attributes   map[string]any
	raw          []string
	line         int
}
type accountRow struct {
	externalID  string
	accountType domain.AccountType
	raw         []string
	line        int
}
type linkRow struct {
	accountExternalID, customerExternalID string
	role                                  domain.AccountRole
	raw                                   []string
	line                                  int
}
type transactionRow struct {
	externalID, customerExternalID, accountExternalID, currency, counterpartyID, counterpartyCountry, channel string
	amount                                                                                                    float64
	direction                                                                                                 domain.TransactionDirection
	transactionType                                                                                           domain.TransactionType
	executedAt                                                                                                time.Time
	metadata                                                                                                  map[string]any
	raw                                                                                                       []string
	line                                                                                                      int
}

func (i *Importer) Run(ctx context.Context, opts Options) (*Report, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	report := &Report{ID: deterministicID("import-run", started.Format(time.RFC3339Nano)+opts.SourceDir), SourceDir: opts.SourceDir, Actor: opts.Actor, DryRun: opts.DryRun || !opts.Apply, Applied: false, StartedAt: started, SourceFiles: map[string]string{}, Counts: map[string]int{}, Outcomes: []RecordOutcome{}}
	bundle, err := readBundle(opts.SourceDir, report)
	if err != nil {
		return report, err
	}
	if err := validateBundle(bundle); err != nil {
		return report, err
	}
	if opts.DryRun || !opts.Apply {
		for _, row := range bundle.customers {
			report.Outcomes = append(report.Outcomes, outcome("customer", row.externalID, customersFile, row.line, row.raw, "accepted", ""))
		}
		for _, row := range bundle.accounts {
			report.Outcomes = append(report.Outcomes, outcome("account", row.externalID, accountsFile, row.line, row.raw, "accepted", ""))
		}
		for _, row := range bundle.links {
			report.Outcomes = append(report.Outcomes, outcome("account_customer", row.accountExternalID+":"+row.customerExternalID, accountCustomersFile, row.line, row.raw, "accepted", ""))
		}
		for _, row := range bundle.transactions {
			report.Outcomes = append(report.Outcomes, outcome("transaction", row.externalID, transactionsFile, row.line, row.raw, "accepted", ""))
		}
		report.Counts = countOutcomes(report.Outcomes)
		report.CompletedAt = time.Now().UTC()
		return report, nil
	}
	if i.Deps.Customers == nil || i.Deps.Accounts == nil || i.Deps.Transactions == nil {
		return report, errors.New("apply requires customer, account, and transaction repositories")
	}
	if err := i.apply(ctx, opts, bundle, report); err != nil {
		return report, err
	}
	report.Applied = true
	report.DryRun = false
	report.Counts = countOutcomes(report.Outcomes)
	report.CompletedAt = time.Now().UTC()
	return report, nil
}

func validateOptions(opts Options) error {
	if strings.TrimSpace(opts.SourceDir) == "" {
		return errors.New("source directory is required")
	}
	if opts.Apply && opts.DryRun {
		return errors.New("--dry-run and --apply are mutually exclusive")
	}
	if opts.OnDuplicate == "" {
		opts.OnDuplicate = DuplicateSkip
	}
	if opts.OnDuplicate != DuplicateSkip && opts.OnDuplicate != DuplicateError {
		return fmt.Errorf("invalid on-duplicate mode %q", opts.OnDuplicate)
	}
	return nil
}

func readBundle(dir string, report *Report) (parsedBundle, error) {
	bundle := parsedBundle{files: map[string]string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return bundle, fmt.Errorf("read source directory: %w", err)
	}
	known := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".csv") {
			return bundle, fmt.Errorf("unsupported source file %q; only fixed CSV inputs are accepted", name)
		}
		if _, ok := fixedHeaders[name]; !ok {
			return bundle, fmt.Errorf("unsupported source file %q", name)
		}
		known[name] = true
	}
	if len(known) == 0 {
		return bundle, errors.New("source directory must contain at least one supported CSV file")
	}
	for name := range known {
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			return bundle, err
		}
		if !utf8.Valid(body) {
			return bundle, fmt.Errorf("%s is not valid UTF-8", name)
		}
		h := sha256.Sum256(body)
		sum := hex.EncodeToString(h[:])
		bundle.files[name] = sum
		report.SourceFiles[name] = sum
		switch name {
		case customersFile:
			bundle.customers, err = parseCustomers(body)
		case accountsFile:
			bundle.accounts, err = parseAccounts(body)
		case accountCustomersFile:
			bundle.links, err = parseLinks(body)
		case transactionsFile:
			bundle.transactions, err = parseTransactions(body)
		}
		if err != nil {
			return bundle, fmt.Errorf("parse %s: %w", name, err)
		}
	}
	return bundle, nil
}

func newCSV(body []byte, name string) (*csvReaderWithLine, error) {
	r := csv.NewReader(strings.NewReader(string(body)))
	r.FieldsPerRecord = -1
	r.ReuseRecord = false
	header, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s is empty", name)
		}
		return nil, err
	}
	if err := validateHeader(name, header); err != nil {
		return nil, err
	}
	return &csvReaderWithLine{Reader: r, line: 1}, nil
}

type csvReaderWithLine struct {
	*csv.Reader
	line int
}

func (r *csvReaderWithLine) Read() ([]string, int, error) {
	row, err := r.Reader.Read()
	r.line++
	return row, r.line, err
}

func validateHeader(name string, got []string) error {
	want := fixedHeaders[name]
	if len(got) != len(want) {
		return fmt.Errorf("%s header has %d columns, want %d fixed columns", name, len(got), len(want))
	}
	for n, value := range got {
		if strings.TrimSpace(value) != want[n] {
			return fmt.Errorf("%s header column %d is %q, want %q", name, n+1, value, want[n])
		}
	}
	return nil
}

func parseCustomers(body []byte) ([]customerRow, error) {
	rr, err := newCSV(body, customersFile)
	if err != nil {
		return nil, err
	}
	var out []customerRow
	for {
		row, line, err := rr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) != len(fixedHeaders[customersFile]) {
			return nil, fmt.Errorf("line %d: wrong column count", line)
		}
		products, err := parseStringArray(row[4])
		if err != nil {
			return nil, fmt.Errorf("line %d product_types_json: %w", line, err)
		}
		attrs, err := parseObject(row[5])
		if err != nil {
			return nil, fmt.Errorf("line %d attributes_json: %w", line, err)
		}
		if row[0] == "" {
			return nil, fmt.Errorf("line %d external_id is required", line)
		}
		out = append(out, customerRow{externalID: row[0], customerType: domain.CustomerType(row[1]), country: row[2], status: domain.CustomerStatus(row[3]), productTypes: products, attributes: attrs, raw: row, line: line})
	}
	return out, nil
}
func parseAccounts(body []byte) ([]accountRow, error) {
	rr, err := newCSV(body, accountsFile)
	if err != nil {
		return nil, err
	}
	var out []accountRow
	for {
		row, line, err := rr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if row[0] == "" {
			return nil, fmt.Errorf("line %d external_id is required", line)
		}
		out = append(out, accountRow{externalID: row[0], accountType: domain.AccountType(row[1]), raw: row, line: line})
	}
	return out, nil
}
func parseLinks(body []byte) ([]linkRow, error) {
	rr, err := newCSV(body, accountCustomersFile)
	if err != nil {
		return nil, err
	}
	var out []linkRow
	for {
		row, line, err := rr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if row[0] == "" || row[1] == "" {
			return nil, fmt.Errorf("line %d account and customer external_id are required", line)
		}
		out = append(out, linkRow{accountExternalID: row[0], customerExternalID: row[1], role: domain.AccountRole(row[2]), raw: row, line: line})
	}
	return out, nil
}
func parseTransactions(body []byte) ([]transactionRow, error) {
	rr, err := newCSV(body, transactionsFile)
	if err != nil {
		return nil, err
	}
	var out []transactionRow
	for {
		row, line, err := rr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		amount, err := strconv.ParseFloat(row[3], 64)
		if err != nil {
			return nil, fmt.Errorf("line %d amount: %w", line, err)
		}
		when, err := time.Parse(time.RFC3339, row[7])
		if err != nil {
			return nil, fmt.Errorf("line %d executed_at must be RFC3339: %w", line, err)
		}
		metadata, err := parseObject(row[11])
		if err != nil {
			return nil, fmt.Errorf("line %d metadata_json: %w", line, err)
		}
		if row[0] == "" || row[1] == "" {
			return nil, fmt.Errorf("line %d transaction and customer external_id are required", line)
		}
		out = append(out, transactionRow{externalID: row[0], customerExternalID: row[1], accountExternalID: row[2], amount: amount, currency: row[4], direction: domain.TransactionDirection(row[5]), transactionType: domain.TransactionType(row[6]), executedAt: when, counterpartyID: row[8], counterpartyCountry: row[9], channel: row[10], metadata: metadata, raw: row, line: line})
	}
	return out, nil
}

func parseStringArray(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return out, nil
}
func parseObject(value string) (map[string]any, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("must be a JSON object")
	}
	return out, nil
}

func validateBundle(b parsedBundle) error {
	seen := map[string]map[string][]string{}
	for _, row := range b.customers {
		if !domain.IsValidCustomerType(row.customerType) {
			return fmt.Errorf("customer %s has invalid customer_type %q", row.externalID, row.customerType)
		}
		if err := duplicatePayload(seen, "customer", row.externalID, row.raw); err != nil {
			return err
		}
	}
	for _, row := range b.accounts {
		if row.accountType != domain.AccountTypeIndividual && row.accountType != domain.AccountTypeJoint {
			return fmt.Errorf("account %s has invalid account_type %q", row.externalID, row.accountType)
		}
		if err := duplicatePayload(seen, "account", row.externalID, row.raw); err != nil {
			return err
		}
	}
	for _, row := range b.transactions {
		if row.amount < 0 {
			return fmt.Errorf("transaction %s amount must be non-negative", row.externalID)
		}
		if err := duplicatePayload(seen, "transaction", row.externalID, row.raw); err != nil {
			return err
		}
	}
	for _, row := range b.links {
		if row.role != domain.AccountRolePrimary && row.role != domain.AccountRoleCoHolder {
			return fmt.Errorf("account_customer %s/%s has invalid role %q", row.accountExternalID, row.customerExternalID, row.role)
		}
		if err := duplicatePayload(seen, "account_customer", row.accountExternalID+":"+row.customerExternalID, row.raw); err != nil {
			return err
		}
	}
	customerIDs, accountIDs := map[string]bool{}, map[string]bool{}
	for _, row := range b.customers {
		customerIDs[row.externalID] = true
	}
	for _, row := range b.accounts {
		accountIDs[row.externalID] = true
	}
	for _, row := range b.links {
		if !customerIDs[row.customerExternalID] {
			return fmt.Errorf("account_customer references unknown customer %q", row.customerExternalID)
		}
		if !accountIDs[row.accountExternalID] {
			return fmt.Errorf("account_customer references unknown account %q", row.accountExternalID)
		}
	}
	for _, row := range b.transactions {
		if row.direction != domain.DirectionInbound && row.direction != domain.DirectionOutbound && row.direction != domain.DirectionInternal {
			return fmt.Errorf("transaction %s has invalid direction %q", row.externalID, row.direction)
		}
		if !customerIDs[row.customerExternalID] {
			return fmt.Errorf("transaction references unknown customer %q", row.customerExternalID)
		}
		if row.accountExternalID != "" && !accountIDs[row.accountExternalID] {
			return fmt.Errorf("transaction references unknown account %q", row.accountExternalID)
		}
	}
	return nil
}

func duplicatePayload(seen map[string]map[string][]string, kind, id string, raw []string) error {
	if seen[kind] == nil {
		seen[kind] = map[string][]string{}
	}
	if previous, ok := seen[kind][id]; ok && strings.Join(previous, "\x00") != strings.Join(raw, "\x00") {
		return fmt.Errorf("conflicting duplicate %s external_id %q", kind, id)
	}
	seen[kind][id] = raw
	return nil
}
func outcome(kind, id, file string, line int, raw []string, status, reason string) RecordOutcome {
	h := sha256.Sum256([]byte(strings.Join(raw, "\x00")))
	return RecordOutcome{EntityType: kind, ExternalID: id, SourceFile: file, Line: line, PayloadSHA256: hex.EncodeToString(h[:]), Status: status, ReasonCode: reason}
}
func countOutcomes(outcomes []RecordOutcome) map[string]int {
	counts := map[string]int{}
	for _, item := range outcomes {
		counts[item.Status]++
	}
	return counts
}

func (i *Importer) apply(ctx context.Context, opts Options, b parsedBundle, report *Report) error {
	customerIDs, accountIDs := map[string]string{}, map[string]string{}
	for _, row := range b.customers {
		id := deterministicID("customer", row.externalID)
		existing, err := i.Deps.Customers.GetByExternalID(ctx, row.externalID)
		if err == nil && existing != nil {
			customerIDs[row.externalID] = existing.ID
			status, reason := duplicateDecision(opts.OnDuplicate, customerPayloadEqual(existing, row), row.raw)
			report.Outcomes = append(report.Outcomes, outcome("customer", row.externalID, customersFile, row.line, row.raw, status, reason))
			if status == "rejected" {
				return fmt.Errorf("customer %s: %s", row.externalID, reason)
			}
			continue
		}
		if err != nil && !isNotFound(err) {
			return err
		}
		c := &domain.Customer{ID: id, ExternalID: row.externalID, CustomerType: row.customerType, CountryCode: row.country, Status: row.status, ProductTypes: row.productTypes, Attributes: row.attributes, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if err := i.Deps.Customers.Create(ctx, c); err != nil {
			return fmt.Errorf("create customer %s: %w", row.externalID, err)
		}
		customerIDs[row.externalID] = c.ID
		report.Outcomes = append(report.Outcomes, outcome("customer", row.externalID, customersFile, row.line, row.raw, "accepted", ""))
	}
	for _, row := range b.accounts {
		id := deterministicID("account", row.externalID)
		if repo, ok := i.Deps.Accounts.(domain.AccountExternalIDRepository); ok {
			existing, err := repo.GetByExternalID(ctx, row.externalID)
			if err == nil && existing != nil {
				accountIDs[row.externalID] = existing.ID
				status, reason := duplicateDecision(opts.OnDuplicate, accountPayloadEqual(existing, row), row.raw)
				report.Outcomes = append(report.Outcomes, outcome("account", row.externalID, accountsFile, row.line, row.raw, status, reason))
				if status == "rejected" {
					return fmt.Errorf("account %s: %s", row.externalID, reason)
				}
				continue
			}
			if err != nil && !isNotFound(err) {
				return err
			}
		}
		a := &domain.Account{ID: id, ExternalID: row.externalID, AccountType: row.accountType}
		if err := i.Deps.Accounts.Create(ctx, a); err != nil {
			return fmt.Errorf("create account %s: %w", row.externalID, err)
		}
		accountIDs[row.externalID] = a.ID
		report.Outcomes = append(report.Outcomes, outcome("account", row.externalID, accountsFile, row.line, row.raw, "accepted", ""))
	}
	for _, row := range b.links {
		if err := i.Deps.Accounts.AddCustomer(ctx, accountIDs[row.accountExternalID], customerIDs[row.customerExternalID], row.role); err != nil {
			return fmt.Errorf("link %s/%s: %w", row.accountExternalID, row.customerExternalID, err)
		}
		report.Outcomes = append(report.Outcomes, outcome("account_customer", row.accountExternalID+":"+row.customerExternalID, accountCustomersFile, row.line, row.raw, "accepted", ""))
	}
	for _, row := range b.transactions {
		customerID := customerIDs[row.customerExternalID]
		var accountID *string
		if row.accountExternalID != "" {
			value := accountIDs[row.accountExternalID]
			accountID = &value
		}
		if repo, ok := i.Deps.Transactions.(domain.TransactionExternalIDRepository); ok {
			existing, err := repo.GetByExternalID(ctx, row.externalID)
			if err == nil && existing != nil {
				status, reason := duplicateDecision(opts.OnDuplicate, transactionPayloadEqual(existing, row), row.raw)
				report.Outcomes = append(report.Outcomes, outcome("transaction", row.externalID, transactionsFile, row.line, row.raw, status, reason))
				if status == "rejected" {
					return fmt.Errorf("transaction %s: %s", row.externalID, reason)
				}
				continue
			}
			if err != nil && !isNotFound(err) {
				return err
			}
		}
		tx := &domain.Transaction{ID: deterministicID("transaction", row.externalID), ExternalID: row.externalID, CustomerID: customerID, AccountID: accountID, Amount: row.amount, Currency: row.currency, Direction: row.direction, TransactionType: row.transactionType, ExecutedAt: row.executedAt, CreatedAt: time.Now().UTC(), CounterpartyID: row.counterpartyID, CounterpartyCountry: row.counterpartyCountry, Channel: row.channel, Metadata: row.metadata}
		if err := i.Deps.Transactions.Create(ctx, tx); err != nil {
			return fmt.Errorf("create transaction %s: %w", row.externalID, err)
		}
		report.Outcomes = append(report.Outcomes, outcome("transaction", row.externalID, transactionsFile, row.line, row.raw, "accepted", ""))
	}
	return nil
}

func duplicateDecision(mode DuplicateMode, samePayload bool, _ []string) (string, string) {
	if samePayload && mode != DuplicateError {
		return "skipped", "duplicate_external_id"
	}
	if samePayload {
		return "rejected", "duplicate_external_id"
	}
	return "rejected", "conflicting_payload"
}

func customerPayloadEqual(existing *domain.Customer, row customerRow) bool {
	if existing == nil {
		return false
	}
	return existing.CustomerType == row.customerType && existing.CountryCode == row.country && existing.Status == row.status && equalJSON(existing.ProductTypes, row.productTypes) && equalJSON(existing.Attributes, row.attributes)
}
func accountPayloadEqual(existing *domain.Account, row accountRow) bool {
	return existing != nil && existing.AccountType == row.accountType
}
func transactionPayloadEqual(existing *domain.Transaction, row transactionRow) bool {
	if existing == nil {
		return false
	}
	return existing.Amount == row.amount && existing.Currency == row.currency && existing.Direction == row.direction && existing.TransactionType == row.transactionType && existing.ExecutedAt.Equal(row.executedAt) && existing.CounterpartyID == row.counterpartyID && existing.CounterpartyCountry == row.counterpartyCountry && existing.Channel == row.channel && equalJSON(existing.Metadata, row.metadata)
}
func equalJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
func isNotFound(err error) bool { var nf *domain.ErrNotFound; return errors.As(err, &nf) }
func deterministicID(kind, externalID string) string {
	h := sha256.Sum256([]byte(kind + "\x00" + externalID))
	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// WriteReport emits a stable, machine-readable report. It is intentionally a
// separate operation so dry-run callers can inspect it without changing state.
func WriteReport(w io.Writer, report *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// ParseSourceDir is a read-only helper used by tests and CLI validation.
func ParseSourceDir(dir string) (*Report, error) {
	return (&Importer{}).Run(context.Background(), Options{SourceDir: dir, DryRun: true})
}
