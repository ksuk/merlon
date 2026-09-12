package demogen

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

const (
	englishTourPath  = "../../../docs/demo-tour.md"
	japaneseTourPath = "../../../website/i18n/ja/docusaurus-plugin-content-docs/current/demo-tour.md"
	readmePath       = "../../../README.md"
)

var uuidRoutePattern = regexp.MustCompile(`/([a-z]+)/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

func TestDemoTourFixedRoutesDescribeOneGeneratedStory(t *testing.T) {
	r := generateOnce(t)
	english := readContractFile(t, englishTourPath)
	japanese := readContractFile(t, japaneseTourPath)

	englishRoutes := fixedRoutes(t, english)
	japaneseRoutes := fixedRoutes(t, japanese)
	for _, resource := range []string{"alerts", "transactions", "customers", "cases"} {
		if englishRoutes[resource] != japaneseRoutes[resource] {
			t.Errorf("%s fixed route differs between English and Japanese tours: %s != %s", resource, englishRoutes[resource], japaneseRoutes[resource])
		}
	}

	customers := make(map[string]domain.Customer, len(r.Customers))
	for _, customer := range r.Customers {
		customers[customer.ID] = customer
	}
	transactions := make(map[string]domain.Transaction, len(r.Transactions))
	for _, transaction := range r.Transactions {
		transactions[transaction.ID] = transaction
	}
	alerts := make(map[string]domain.Alert, len(r.Alerts))
	for _, alert := range r.Alerts {
		alerts[alert.ID] = alert
	}
	cases := make(map[string]domain.Case, len(r.Cases))
	for _, kase := range r.Cases {
		cases[kase.ID] = kase
	}

	customerID := englishRoutes["customers"]
	if _, ok := customers[customerID]; !ok {
		t.Fatalf("tour customer %s is absent from generated customers", customerID)
	}
	transaction, ok := transactions[englishRoutes["transactions"]]
	if !ok {
		t.Fatalf("tour transaction %s is absent from generated transactions", englishRoutes["transactions"])
	}
	alert, ok := alerts[englishRoutes["alerts"]]
	if !ok {
		t.Fatalf("tour alert %s is absent from generated alerts", englishRoutes["alerts"])
	}
	kase, ok := cases[englishRoutes["cases"]]
	if !ok {
		t.Fatalf("tour case %s is absent from generated cases", englishRoutes["cases"])
	}

	if transaction.CustomerID != customerID {
		t.Errorf("tour transaction belongs to customer %s, want %s", transaction.CustomerID, customerID)
	}
	if alert.CustomerID != customerID {
		t.Errorf("tour alert belongs to customer %s, want %s", alert.CustomerID, customerID)
	}
	if kase.CustomerID != customerID {
		t.Errorf("tour case belongs to customer %s, want %s", kase.CustomerID, customerID)
	}
	if !containsID(alert.TransactionIDs, transaction.ID) {
		t.Errorf("tour alert %s does not reference tour transaction %s", alert.ID, transaction.ID)
	}
	if !containsID(kase.AlertIDs, alert.ID) {
		t.Errorf("tour case %s does not reference tour alert %s", kase.ID, alert.ID)
	}
	if !kase.STRCandidate || !domain.IsCaseUnresolved(kase.Status) {
		t.Errorf("tour case %s is not an active STR candidate: status=%s str_candidate=%t", kase.ID, kase.Status, kase.STRCandidate)
	}
}

func TestDemoDocumentationCountsMatchGeneratedDataset(t *testing.T) {
	r := generateOnce(t)
	tests := []struct {
		name            string
		path            string
		customerPattern string
		alertPattern    string
	}{
		{name: "English tour", path: englishTourPath, customerPattern: `([0-9,]+) (?:synthetic )?customers`, alertPattern: `([0-9,]+) alerts`},
		{name: "Japanese tour", path: japaneseTourPath, customerPattern: `顧客(?:約)?([0-9,]+)件`, alertPattern: `アラート([0-9,]+)件`},
		{name: "README", path: readmePath, customerPattern: `([0-9,]+) customers`, alertPattern: `([0-9,]+) alerts`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := readContractFile(t, tt.path)
			if got := documentedCount(t, content, tt.customerPattern); got != len(r.Customers) {
				t.Errorf("documented customers = %d, generated = %d", got, len(r.Customers))
			}
			if got := documentedCount(t, content, tt.alertPattern); got != len(r.Alerts) {
				t.Errorf("documented alerts = %d, generated = %d", got, len(r.Alerts))
			}
		})
	}
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func fixedRoutes(t *testing.T, content string) map[string]string {
	t.Helper()
	routes := map[string]string{}
	for _, match := range uuidRoutePattern.FindAllStringSubmatch(content, -1) {
		if _, wanted := map[string]bool{"alerts": true, "transactions": true, "customers": true, "cases": true}[match[1]]; wanted {
			routes[match[1]] = match[2]
		}
	}
	for _, resource := range []string{"alerts", "transactions", "customers", "cases"} {
		if routes[resource] == "" {
			t.Fatalf("tour has no fixed /%s/<uuid> route", resource)
		}
	}
	return routes
}

func documentedCount(t *testing.T, content, pattern string) int {
	t.Helper()
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(content)
	if len(match) != 2 {
		t.Fatalf("documented count does not match %q", pattern)
	}
	value, err := strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
	if err != nil {
		t.Fatalf("parse documented count %q: %v", match[1], err)
	}
	return value
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestDemoTourContractTestPatternsRemainSpecific(t *testing.T) {
	if uuidRoutePattern.MatchString("/alerts/not-a-uuid") {
		t.Fatal("fixed-route pattern accepted a non-UUID")
	}
	if got := fmt.Sprint(uuidRoutePattern.FindStringSubmatch("/alerts/419d1314-654e-5375-bfb7-9fcea10fcd53")[1:]); got != "[alerts 419d1314-654e-5375-bfb7-9fcea10fcd53]" {
		t.Fatalf("fixed-route pattern groups = %s", got)
	}
}
