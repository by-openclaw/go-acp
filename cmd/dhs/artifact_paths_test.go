package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The ADR-0028 path-composer pins (unit-5 layer): every composed
// default must match the ADR table shapes exactly — layout drift
// fails the build here, not in the field.
func TestArtifactPaths_ADR0028Shapes(t *testing.T) {
	ts := time.Date(2026, 8, 17, 21, 30, 0, 0, time.UTC)

	if got := snapshotDir("cerebrum-nb", "0.0.0.0"); !strings.HasSuffix(got,
		filepath.Join("snapshots", "cerebrum-nb", "0.0.0.0")) {
		t.Fatalf("snapshotDir RM = %q", got)
	}
	if got := snapshotDir("acp2", "10.44.72.28"); !strings.HasSuffix(got,
		filepath.Join("snapshots", "acp2", "10.44.72.28")) {
		t.Fatalf("snapshotDir device = %q", got)
	}

	if got := defaultCapturePath("cerebrum-nb", "10.44.55.39", "export", "rm", ts); !strings.HasSuffix(got,
		filepath.Join("captures", "cerebrum-nb", "10.44.55.39", "export-rm-20260817T2130Z.jsonl")) {
		t.Fatalf("capture path with scope = %q", got)
	}
	if got := defaultCapturePath("acp1", "10.100.0.103", "walk", "", ts); !strings.HasSuffix(got,
		filepath.Join("captures", "acp1", "10.100.0.103", "walk-20260817T2130Z.jsonl")) {
		t.Fatalf("capture path no scope = %q", got)
	}

	if got := defaultLogPath("cerebrum-nb", "10.44.55.39", "import"); !strings.HasSuffix(got,
		filepath.Join(".cache", "logs", "cerebrum-nb", "10.44.55.39", "import.log")) {
		t.Fatalf("log path = %q", got)
	}

	// IPv6 / Windows-illegal characters sanitise, never split paths.
	if got := snapshotDir("amwa", "fd00::1"); strings.Contains(filepath.Base(got), ":") {
		t.Fatalf("IPv6 key not sanitised: %q", got)
	}
}

func TestFacetFile(t *testing.T) {
	if got := facetFile("d", "", "xpoint"); got != filepath.Join("d", "xpoint.csv") {
		t.Fatalf("plain facet = %q", got)
	}
	if got := facetFile("d", "noc", "xpoint"); got != filepath.Join("d", "noc-xpoint.csv") {
		t.Fatalf("prefixed facet = %q", got)
	}
}

// TestArtifactPaths_AcceptanceMatrix is the ADR-0028 acceptance table
// in executable form (#703 unit 5): one row per proto × folder-class ×
// artifact. A change to any composed default fails HERE first — the
// CI layer of the three-layer verification plan.
func TestArtifactPaths_AcceptanceMatrix(t *testing.T) {
	ts := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)

	snapshotRows := []struct {
		proto, key string
		facets     []string
	}{
		// Matrix-domain folder classes (facet sets per connector
		// capability — a missing facet is a protocol fact).
		{"cerebrum-nb", "0.0.0.0", []string{"xpoint", "src", "dst", "level", "lock", "cat-src", "cat-dst", "cat-mixed"}}, // Route Master
		{"cerebrum-nb", "10.44.72.24", []string{"xpoint", "src", "dst", "level"}},                                       // physical router
		{"probel-sw08p", "10.44.72.27", []string{"xpoint", "src", "dst", "protect"}},                                    // lock/protect in-protocol
		// Tree/DM folder classes: one facet, params.
		{"cerebrum-nb", "10.44.72.28", nil},
		{"acp1", "10.100.0.103", nil},
		{"acp2", "10.100.0.103", nil},
		{"emberplus", "10.6.239.103", nil},
		{"osc-v11", "10.100.0.104", nil},
	}
	for _, row := range snapshotRows {
		dir := snapshotDir(row.proto, row.key)
		want := filepath.Join("snapshots", row.proto, row.key)
		if !strings.HasSuffix(dir, want) {
			t.Fatalf("snapshotDir(%s,%s) = %q, want suffix %q", row.proto, row.key, dir, want)
		}
		for _, facet := range row.facets {
			if got := facetFile(dir, "", facet); got != filepath.Join(dir, facet+".csv") {
				t.Fatalf("facet %s in %s = %q", facet, dir, got)
			}
		}
	}

	captureRows := []struct{ proto, key, verb, scope, want string }{
		{"cerebrum-nb", "10.44.55.39", "extract", "10.44.72.28", "extract-10.44.72.28-20260818T0900Z.jsonl"},
		{"acp2", "10.100.0.103", "walk", "", "walk-20260818T0900Z.jsonl"},
		{"probel-sw08p", "10.44.72.27", "import", "", "import-20260818T0900Z.jsonl"},
		{"tsl-v50", "10.100.0.106", "listen", "", "listen-20260818T0900Z.jsonl"},
	}
	for _, row := range captureRows {
		got := defaultCapturePath(row.proto, row.key, row.verb, row.scope, ts)
		want := filepath.Join("captures", row.proto, row.key, row.want)
		if !strings.HasSuffix(got, want) {
			t.Fatalf("capture(%s,%s,%s) = %q, want suffix %q", row.proto, row.verb, row.scope, got, want)
		}
	}

	logRows := []struct{ proto, key, verb string }{
		{"cerebrum-nb", "10.44.55.39", "export"},
		{"acp1", "10.100.0.103", "watch"},
	}
	for _, row := range logRows {
		got := defaultLogPath(row.proto, row.key, row.verb)
		want := filepath.Join(".cache", "logs", row.proto, row.key, row.verb+".log")
		if !strings.HasSuffix(got, want) {
			t.Fatalf("log(%s,%s) = %q, want suffix %q", row.proto, row.verb, got, want)
		}
	}

	// Key rules: RM sentinel is a valid key; ports never leak into keys.
	if hostOnly("10.44.55.39:40009") != "10.44.55.39" || hostOnly("0.0.0.0") != "0.0.0.0" {
		t.Fatal("hostOnly key rule violated")
	}
}
