package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ksuk/merlon/api/internal/demogen"
)

func main() {
	output := flag.String("output", "deploy/seed/demo/demo_seed.sql", "output SQL path")
	seed := flag.Int64("seed", demogen.DefaultSeed, "deterministic seed")
	customers := flag.Int("customers", demogen.DefaultCustomers, "number of customers")
	transactions := flag.Int("transactions-per-customer", demogen.DefaultTransactionsPerCustomer, "transactions per customer")
	flag.Parse()
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		panic(err)
	}
	f, err := os.Create(*output)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := demogen.WriteSQL(f, demogen.Options{Seed: *seed, Customers: *customers, TransactionsPerCustomer: *transactions}); err != nil {
		panic(err)
	}
	fmt.Printf("generated %s (seed=%d customers=%d transactions_per_customer=%d)\n", *output, *seed, *customers, *transactions)
}
