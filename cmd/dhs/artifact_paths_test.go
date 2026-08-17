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
