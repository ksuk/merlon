package retention

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deleteStatement matches a DELETE that names a table, in either the plain or
// the CTE form the purgers use.
var deleteStatement = regexp.MustCompile(`(?i)DELETE\s+FROM\s+([a-z_][a-z0-9_]*)`)

// TestProvenanceReferencedTablesAreNeverPurged enforces the retention
// invariant ADR-0025 states: an alert's retention period is the lower bound on
// the retention of the rule version its provenance names.
//
// The check is deliberately not a foreign key. The scenario an alert names is
// not always a stored rule -- the native engine also loads scenarios from the
// configuration root -- so a constraint would refuse legitimate alerts. Instead
// the invariant is verified against the purge code itself, the same judgement
// migration 049 made for the customer guard, so a future purge target that
// starts deleting rule versions fails here rather than in production after the
// evidence is gone.
func TestProvenanceReferencedTablesAreNeverPurged(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	protected := map[string]bool{}
	for _, table := range ProvenanceReferencedTables {
		protected[table] = true
	}

	checked := 0
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		checked++

		for _, match := range deleteStatement.FindAllStringSubmatch(string(content), -1) {
			if protected[strings.ToLower(match[1])] {
				t.Errorf(
					"%s deletes from %q, which alert provenance references. "+
						"Purging a rule version while an alert still names it destroys the only "+
						"artifact that explains why that alert fired.",
					path, match[1],
				)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no purge sources were inspected, so this guard proved nothing")
	}
}

// TestProvenanceReferencedTablesIsNotEmpty stops the guard above from being
// silently disarmed by emptying the list it checks against.
func TestProvenanceReferencedTablesIsNotEmpty(t *testing.T) {
	if len(ProvenanceReferencedTables) == 0 {
		t.Fatal("ProvenanceReferencedTables is empty; the provenance retention guard checks nothing")
	}
}
