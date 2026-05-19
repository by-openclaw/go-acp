package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPercentile_BasicCases pins the nearest-rank percentile helper
// used to derive p50/p95/p99 from the sorted latency slice.
func TestPercentile_BasicCases(t *testing.T) {
	sorted := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
		60 * time.Millisecond,
		70 * time.Millisecond,
		80 * time.Millisecond,
		90 * time.Millisecond,
		100 * time.Millisecond,
	}
	cases := []struct {
		p    int
		want time.Duration
	}{
		{0, 10 * time.Millisecond},
		{50, 60 * time.Millisecond},
		{95, 100 * time.Millisecond},
		{99, 100 * time.Millisecond},
		{100, 100 * time.Millisecond},
	}
	for _, tc := range cases {
		got := percentile(sorted, tc.p)
		if got != tc.want {
			t.Errorf("percentile(sorted, %d) = %s; want %s", tc.p, got, tc.want)
		}
	}
}

// TestPercentile_Empty asserts the helper does not panic on an empty
// slice (the recovery profile could plausibly return zero samples if
// every probe errored).
func TestPercentile_Empty(t *testing.T) {
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile(nil, 50) = %s; want 0", got)
	}
}

// TestPercentile_Single asserts every percentile of a one-element
// slice returns that element.
func TestPercentile_Single(t *testing.T) {
	s := []time.Duration{42 * time.Millisecond}
	for _, p := range []int{0, 50, 95, 99, 100} {
		if got := percentile(s, p); got != 42*time.Millisecond {
			t.Errorf("percentile([42ms], %d) = %s; want 42ms", p, got)
		}
	}
}

// TestWriteEmberplusBenchCSV_NewFileWritesHeader pins the schema: when the CSV
// file does not exist, the header line is emitted before the first
// row so downstream tools see column names.
func TestWriteEmberplusBenchCSV_NewFileWritesHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bench.csv")
	row := emberplusBenchCSVRow{
		Profile: "rfc2544-latency", N: 1000, Op: "absolute",
		WallMs: 2543, OpsPerSec: 393.2, Errors: 0,
		P50us: 2150, P95us: 4521, P99us: 8731,
		RecoveryMs: -1,
	}
	if err := writeEmberplusBenchCSV(path, row); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "profile,n,op,wall_ms,ops_per_sec,errors,p50_us,p95_us,p99_us,recovery_ms") {
		t.Errorf("header missing: %q", out)
	}
	if !strings.Contains(out, "rfc2544-latency,1000,absolute,2543,393.20,0,2150,4521,8731,-1") {
		t.Errorf("row missing: %q", out)
	}
}

// TestWriteEmberplusBenchCSV_AppendNoDuplicateHeader pins that subsequent writes
// against the same file skip the header — concatenated runs produce
// one header followed by N rows, not interleaved.
func TestWriteEmberplusBenchCSV_AppendNoDuplicateHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bench.csv")
	row1 := emberplusBenchCSVRow{Profile: "first", N: 1, Op: "connect", RecoveryMs: -1}
	row2 := emberplusBenchCSVRow{Profile: "second", N: 2, Op: "absolute", RecoveryMs: -1}
	if err := writeEmberplusBenchCSV(path, row1); err != nil {
		t.Fatal(err)
	}
	if err := writeEmberplusBenchCSV(path, row2); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Count(out, "profile,n,op,wall_ms") != 1 {
		t.Errorf("header written %d times (want 1): %q", strings.Count(out, "profile,n,op,wall_ms"), out)
	}
	if !strings.Contains(out, "first,1") || !strings.Contains(out, "second,2") {
		t.Errorf("rows missing: %q", out)
	}
}
