package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/errcode"
	"dhs/internal/wiretrace"
)

// TestResolveReportFormat covers the --report extension dispatch and
// the invalid-format / stdout / file modes documented in R23 #488.
func TestResolveReportFormat(t *testing.T) {
	tests := []struct {
		in   string
		want string
		err  bool
	}{
		{"", "", false},
		{"-", "md", false},
		{"report.md", "md", false},
		{"REPORT.JSON", "json", false},
		{"path/to/r.md", "md", false},
		{"report.txt", "", true},
		{"report", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := resolveReportFormat(tc.in)
			if tc.err {
				if err == nil {
					t.Errorf("want error, got nil")
				} else if !errors.Is(err, errReportInvalidFormat) {
					t.Errorf("want errReportInvalidFormat chain, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyFromMessage_KnownLayers verifies the layer + code prefix
// extraction across every documented layer.
func TestClassifyFromMessage_KnownLayers(t *testing.T) {
	cases := []struct {
		msg       string
		wantLayer string
		wantCode  string
	}{
		{"s101:crc-mismatch: CRC bad at offset 5", "s101", "s101:crc-mismatch"},
		{"ber:short-payload: TLV truncated", "ber", "ber:short-payload"},
		{"glow:bad-tag: unknown APPLICATION 31", "glow", "glow:bad-tag"},
		{"matrix:target-locked: target 2", "matrix", "matrix:target-locked"},
		{"random text without prefix", "validate", ""},
		{"s101", "validate", ""}, // no colon -> not a prefix
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			gotLayer, gotCode := classifyFromMessage(tc.msg)
			if gotLayer != tc.wantLayer || gotCode != tc.wantCode {
				t.Errorf("classify(%q) = (%q,%q); want (%q,%q)",
					tc.msg, gotLayer, gotCode, tc.wantLayer, tc.wantCode)
			}
		})
	}
}

// TestRenderValidateReport_AllPass asserts the byLayer counters are
// initialised to total-frames pass / zero fail on a clean capture.
func TestRenderValidateReport_AllPass(t *testing.T) {
	r := &consumer.ValidateReport{
		TramesProcessed: 100,
		PerDirection: map[wiretrace.Direction]int{
			wiretrace.DirectionRx: 50, wiretrace.DirectionTx: 50,
		},
	}
	out := renderValidateReport("walk-happy.jsonl", nil, r,
		time.Now(), time.Now().Add(time.Second))
	if out.Pass != 100 || out.Fail != 0 {
		t.Errorf("pass/fail = %d/%d; want 100/0", out.Pass, out.Fail)
	}
	for _, l := range []string{"s101", "ber", "glow", "stream"} {
		lc := out.ByLayer[l]
		if lc.Pass != 100 || lc.Fail != 0 {
			t.Errorf("byLayer[%s] = %+v; want pass=100 fail=0", l, lc)
		}
	}
}

// TestRenderValidateReport_FailureRoute asserts a glow error routes
// to byLayer.glow.fail and surfaces in Failures with code + layer.
func TestRenderValidateReport_FailureRoute(t *testing.T) {
	trames := []wiretrace.Trame{
		{Hex: "feff0a"}, // index 0
		{Hex: "fe0001"}, // index 1
	}
	r := &consumer.ValidateReport{
		TramesProcessed: 2,
		Errors: []consumer.ValidateError{
			{TrameIndex: 1, Err: "glow:bad-tag: unknown APPLICATION 31 at offset 7 in frame 1"},
		},
	}
	out := renderValidateReport("bad.jsonl", trames, r,
		time.Now(), time.Now().Add(time.Second))
	if out.Pass != 1 || out.Fail != 1 {
		t.Errorf("pass/fail = %d/%d; want 1/1", out.Pass, out.Fail)
	}
	if glow := out.ByLayer["glow"]; glow.Pass != 1 || glow.Fail != 1 {
		t.Errorf("glow layer = %+v; want pass=1 fail=1", glow)
	}
	if len(out.Failures) != 1 {
		t.Fatalf("failures count = %d; want 1", len(out.Failures))
	}
	f := out.Failures[0]
	if f.Layer != "glow" || f.Code != "glow:bad-tag" {
		t.Errorf("failure = %+v; want layer=glow code=glow:bad-tag", f)
	}
	if f.RawHex != "fe0001" {
		t.Errorf("raw_hex = %q; want fe0001", f.RawHex)
	}
}

// TestRenderMarkdownReport_Deterministic asserts the Markdown output
// is byte-deterministic — same input always produces the same output
// so the report file is git-diffable across runs.
func TestRenderMarkdownReport_Deterministic(t *testing.T) {
	r := validateReport{
		File: "walk.jsonl", Frames: 100, Pass: 100, Fail: 0,
		Started: time.Date(2026, 5, 17, 13, 22, 1, 0, time.UTC),
		Ended:   time.Date(2026, 5, 17, 13, 22, 4, 0, time.UTC),
		ByLayer: map[string]layerCounts{
			"s101": {Pass: 100}, "ber": {Pass: 100}, "glow": {Pass: 100}, "stream": {Pass: 100},
		},
	}
	a := renderMarkdownReport(r)
	b := renderMarkdownReport(r)
	if a != b {
		t.Fatal("markdown render is non-deterministic")
	}
	for _, want := range []string{
		"# Validation report — walk.jsonl",
		"- Frames: 100",
		"- Pass:   100 (100.0%)",
		"## Per-layer pass rate",
		"| ber | 100 | 0 |",
		"## Failures",
		"_None — all frames decoded cleanly._",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in:\n%s", want, a)
		}
	}
}

// TestWriteValidateReport_JSONRoundtrip writes a JSON report to a
// temp file and reads it back. The schema must match the spec.
func TestWriteValidateReport_JSONRoundtrip(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "report.json")
	r := validateReport{
		File: "x.jsonl", Frames: 10, Pass: 10, Fail: 0,
		Started: time.Now().UTC(), Ended: time.Now().UTC(),
		ByLayer: map[string]layerCounts{
			"s101": {Pass: 10}, "ber": {Pass: 10}, "glow": {Pass: 10}, "stream": {Pass: 10},
		},
	}
	if err := writeValidateReport(r, tmp, "json"); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf)
	}
	for _, key := range []string{"file", "frames", "pass", "fail", "started", "ended", "byLayer", "failures"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
}

// TestErrors_Codes asserts the validate error sentinels surface the
// expected typed codes.
func TestValidateErrors_Codes(t *testing.T) {
	for _, tc := range []struct {
		err   error
		layer errcode.Layer
		name  string
		class errcode.Class
	}{
		{errReportInvalidFormat, errcode.LayerValidation, "invalid-report-format", errcode.ClassUsage},
		{errReportTargetUnwritable, errcode.LayerTransport, "report-target-unwritable", errcode.ClassRuntime},
		{errInputNotFound, errcode.LayerTransport, "input-not-found", errcode.ClassRuntime},
	} {
		c := errcode.From(tc.err)
		if c == nil || c.Layer != tc.layer || c.Name != tc.name || c.Class != tc.class {
			t.Errorf("err %v: got %+v; want layer=%s name=%s class=%v",
				tc.err, c, tc.layer, tc.name, tc.class)
		}
	}
}
