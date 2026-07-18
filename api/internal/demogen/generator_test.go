package demogen

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteSQLIsDeterministicAndSynthetic(t *testing.T) {
	var one, two bytes.Buffer
	opts := Options{Seed: 20260701, Customers: 3, TransactionsPerCustomer: 2}
	if err := WriteSQL(&one, opts); err != nil {
		t.Fatal(err)
	}
	if err := WriteSQL(&two, opts); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one.Bytes(), two.Bytes()) {
		t.Fatal("same seed produced different SQL")
	}
	for _, marker := range []string{"DEMO-CUSTOMER-0001", "DEMO-TXN-000001", "Synthetic demo alert", "Synthetic investigation story"} {
		if !strings.Contains(one.String(), marker) {
			t.Errorf("generated SQL lacks %q", marker)
		}
	}
	if UUID(opts.Seed, "customer", 0) == UUID(opts.Seed+1, "customer", 0) {
		t.Fatal("seed does not affect UUID")
	}
}
