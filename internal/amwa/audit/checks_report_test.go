package audit

import (
	"strings"
	"testing"
)

// TestStuckCursorLineWithoutPath: an exporter line that says the cursor
// stuck but not where still names the query API, so the defect is
// reported rather than dropped for want of a path.
func TestStuckCursorLineWithoutPath(t *testing.T) {
	line := "WARN  paging cursor did not advance"
	if got := pathOfStuckLine(line); got != "query" {
		t.Errorf("pathOfStuckLine = %q, want query", got)
	}
	h := mk("registry", nil)
	h.Report = []string{line}
	f := has(t, checkTransportReport(h), "NMOS-QUERY-PAGING-STUCK")
	if !strings.Contains(f.Detail, "for query") {
		t.Errorf("the finding should fall back to naming the API: %q", f.Detail)
	}
}

// TestKindOfPathWithoutSlash: a bare collection name is its own kind.
func TestKindOfPathWithoutSlash(t *testing.T) {
	if got := kindOfPath("nodes"); got != "nodes" {
		t.Errorf("kindOfPath(nodes) = %q", got)
	}
	if got := kindOfPath("/x-nmos/query/v1.3/flows"); got != "flows" {
		t.Errorf("kindOfPath = %q, want flows", got)
	}
}

// TestPageLineWithUnparseableIncrement: a page record whose increment
// does not fit an integer cannot be reasoned about and is left out of
// the truncation evidence rather than crashing the audit or inventing
// a short walk.
func TestPageLineWithUnparseableIncrement(t *testing.T) {
	got := truncatedKinds([]string{
		"200   /x-nmos/query/v1.3/nodes  (page 1, +99999999999999999999999, total 100)",
		"200   /x-nmos/query/v1.3/nodes  (page 2, +0, total 100)",
	})
	if len(got) != 0 {
		t.Errorf("an unparseable increment must not count as a full page: %v", got)
	}
}
