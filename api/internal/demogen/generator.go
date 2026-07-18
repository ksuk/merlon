package demogen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSeed                    int64 = 20260701
	DefaultCustomers                     = 1000
	DefaultTransactionsPerCustomer       = 48
)

var anchor = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

type Options struct {
	Seed                    int64
	Customers               int
	TransactionsPerCustomer int
}

func (o Options) withDefaults() Options {
	if o.Seed == 0 {
		o.Seed = DefaultSeed
	}
	if o.Customers <= 0 {
		o.Customers = DefaultCustomers
	}
	if o.TransactionsPerCustomer <= 0 {
		o.TransactionsPerCustomer = DefaultTransactionsPerCustomer
	}
	return o
}

// WriteSQL emits deterministic INSERT statements. The reset command supplies
// the transaction and advisory lock, so this file deliberately contains no
// BEGIN/COMMIT and is safe to regenerate during an image build.
func WriteSQL(w io.Writer, options Options) error {
	o := options.withDefaults()
	if o.Customers > 5000 || o.TransactionsPerCustomer > 200 {
		return fmt.Errorf("demo dimensions exceed safety limits")
	}
	write := func(format string, args ...any) error { _, err := fmt.Fprintf(w, format, args...); return err }
	if err := write("-- Merlon demo dataset; seed=%d anchor=%s\n", o.Seed, anchor.Format(time.RFC3339)); err != nil {
		return err
	}
	if err := write("SET TIME ZONE 'UTC';\n"); err != nil {
		return err
	}

	for i := 0; i < o.Customers; i++ {
		id := UUID(o.Seed, "customer", i)
		tier := []string{"low", "medium", "high"}[i%3]
		score := 1.0 + float64(i%41)/10.0
		created := anchor.Add(-time.Duration(365-(i%300)) * 24 * time.Hour)
		attrs, _ := json.Marshal(map[string]any{
			"name":      fmt.Sprintf("Merlon Demo Customer %04d", i+1),
			"branch":    fmt.Sprintf("DEMO-%02d", i%12+1),
			"synthetic": true,
		})
		if err := write("INSERT INTO customers (id, external_id, customer_type, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at) VALUES ('%s','DEMO-CUSTOMER-%04d','%s','%s'::jsonb,%.2f,'%s','%s','%s','%s');\n", id, i+1, customerType(i), string(attrs), score, tier, anchor.Add(-time.Duration(i%30)*24*time.Hour).Format(time.RFC3339), created.Format(time.RFC3339), anchor.Format(time.RFC3339)); err != nil {
			return err
		}
		if err := write("INSERT INTO customer_score_history (id, customer_id, score, tier, factors, rule_set_id, rule_set_version, scored_at) VALUES ('%s','%s',%.2f,'%s','{\"synthetic\":true}'::jsonb,'demo-cdd',1,'%s');\n", UUID(o.Seed, "score", i), id, score, tier, anchor.Add(-time.Duration(i%30)*24*time.Hour).Format(time.RFC3339)); err != nil {
			return err
		}
	}

	for i := 0; i < o.Customers; i++ {
		customerID := UUID(o.Seed, "customer", i)
		for j := 0; j < o.TransactionsPerCustomer; j++ {
			id := UUID(o.Seed, "transaction", i*o.TransactionsPerCustomer+j)
			executed := anchor.Add(-time.Duration((i*7+j)%365) * 24 * time.Hour).Add(time.Duration(j) * time.Hour)
			direction := "outbound"
			if j%4 == 0 {
				direction = "inbound"
			}
			amount := 10000 + (i*791+j*313)%9000000
			if err := write("INSERT INTO transactions (id, customer_id, external_id, amount, currency, direction, counterparty_country, channel, executed_at, created_at) VALUES ('%s','%s', 'DEMO-TXN-%06d', %d, 'JPY', '%s', '%s', '%s', '%s', '%s');\n", id, customerID, i*o.TransactionsPerCustomer+j+1, amount, direction, []string{"JP", "SG", "GB", "US", "AU"}[(i+j)%5], []string{"online", "branch", "swift", "atm"}[j%4], executed.Format(time.RFC3339), executed.Format(time.RFC3339)); err != nil {
				return err
			}
		}
	}

	alertCount := o.Customers / 10
	if alertCount < 6 {
		alertCount = 6
	}
	for i := 0; i < alertCount; i++ {
		customer := i * 10
		alertID := UUID(o.Seed, "alert", i)
		transaction := UUID(o.Seed, "transaction", customer*o.TransactionsPerCustomer+1)
		severity := []string{"low", "medium", "high", "critical"}[i%4]
		status := []string{"open", "investigating", "escalated"}[i%3]
		if err := write("INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at) VALUES ('%s','%s','DEMO-SCENARIO-%02d','%s','%s',%.2f,'Synthetic demo alert %04d',ARRAY['%s'::uuid],'%s','%s','%s');\n", alertID, UUID(o.Seed, "customer", customer), i%6+1, severity, status, 40.0+float64(i%55), i+1, transaction, anchor.Add(-time.Duration(i%45)*24*time.Hour).Format(time.RFC3339), anchor.Format(time.RFC3339), anchor.Format(time.RFC3339)); err != nil {
			return err
		}
	}

	caseCount := o.Customers / 40
	if caseCount < 6 {
		caseCount = 6
	}
	for i := 0; i < caseCount; i++ {
		customer := (i * 40) % o.Customers
		caseID := fmt.Sprintf("demo-case-%03d", i+1)
		alertID := UUID(o.Seed, "alert", (i*4)%alertCount)
		if err := write("INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at) VALUES ('%s','%s',ARRAY['%s'],'%s','%s','demo-reviewer-%02d','Synthetic investigation story %03d','%s','%s');\n", caseID, UUID(o.Seed, "customer", customer), alertID, []string{"open", "investigating", "escalated"}[i%3], []string{"low", "medium", "high"}[i%3], i%8+1, i+1, anchor.Add(-time.Duration(i%30)*24*time.Hour).Format(time.RFC3339), anchor.Format(time.RFC3339)); err != nil {
			return err
		}
		if err := write("INSERT INTO case_notes (id, case_id, author, content, created_at) VALUES ('demo-note-%03d','%s','demo-reviewer-%02d','Synthetic note for the public demonstration','%s');\n", i+1, caseID, i%8+1, anchor.Format(time.RFC3339)); err != nil {
			return err
		}
	}

	for i := 0; i < 100; i++ {
		if err := write("INSERT INTO audit_logs (user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at) VALUES ('demo-seed','seed','customer','%s','{\"synthetic\":true}'::jsonb,'192.0.2.10','merlon-demo-seed','%s');\n", UUID(o.Seed, "customer", i%o.Customers), anchor.Add(-time.Duration(i)*time.Hour).Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func UUID(seed int64, kind string, index int) string {
	h := sha256.Sum256([]byte(strconv.FormatInt(seed, 10) + ":" + kind + ":" + strconv.Itoa(index)))
	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

func customerType(i int) string {
	return []string{"individual", "corporate_domestic", "corporate_foreign"}[i%3]
}

// Name is kept here so the CLI and acceptance tooling can display the exact
// fixed story IDs without parsing SQL.
func StoryIDs() []string {
	return strings.Split("demo-case-001,demo-case-002,demo-case-003,demo-case-004,demo-case-005,demo-case-006", ",")
}
