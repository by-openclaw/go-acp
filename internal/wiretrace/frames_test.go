package wiretrace_test

import (
	"bytes"
	"strings"
	"testing"

	"dhs/internal/wiretrace"
)

func TestReadTrames_basic(t *testing.T) {
	const input = `{"schema_version":1,"dir":"tx","hex":"c635020001020003"}
{"schema_version":1,"dir":"rx","hex":"c635020101020003","note":"reply_v1"}
`
	got, err := wiretrace.ReadTrames(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d trames, want 2", len(got))
	}
	if got[0].Direction != wiretrace.DirectionTx {
		t.Errorf("trame 0 dir = %q, want tx", got[0].Direction)
	}
	if got[1].Note != "reply_v1" {
		t.Errorf("trame 1 note = %q, want reply_v1", got[1].Note)
	}
}

// TestReadTrames_metaRecordSkipped pins the ADR-0028 contract: a
// hex-less line carrying `meta` is provenance, not traffic — skipped
// without error, whatever position it appears in.
func TestReadTrames_metaRecordSkipped(t *testing.T) {
	const input = `{"schema_version":1,"ts":"2026-08-18T00:00:00Z","meta":{"cli":"dhs consumer cerebrum-nb export h --pass ***","binary_sha256":"sha256:ab","proto":"cerebrum-nb","verb":"export"}}
{"schema_version":1,"dir":"tx","hex":"c635"}
`
	got, err := wiretrace.ReadTrames(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadTrames with meta line: %v", err)
	}
	if len(got) != 1 || got[0].Hex != "c635" {
		t.Fatalf("got %d trames (%+v), want the 1 traffic line", len(got), got)
	}
}

func TestReadTrames_missingRequired(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"missing hex", `{"schema_version":1,"dir":"tx"}`},
		{"missing dir", `{"schema_version":1,"hex":"abcd"}`},
		{"bad dir", `{"schema_version":1,"dir":"both","hex":"abcd"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := wiretrace.ReadTrames(strings.NewReader(tc.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestReadTrames_schemaVersionTooHigh(t *testing.T) {
	const input = `{"schema_version":99,"dir":"tx","hex":"abcd"}`
	_, err := wiretrace.ReadTrames(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected schema_version rejection, got nil")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("error should mention schema_version: %v", err)
	}
}

func TestWriteTrames_setsSchemaVersion(t *testing.T) {
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: "c635"},
	}
	var buf bytes.Buffer
	if err := wiretrace.WriteTrames(&buf, trames); err != nil {
		t.Fatalf("WriteTrames: %v", err)
	}
	if !strings.Contains(buf.String(), `"schema_version":1`) {
		t.Errorf("WriteTrames did not set schema_version: %s", buf.String())
	}
}

func TestRoundTrip(t *testing.T) {
	in := []wiretrace.Trame{
		{SchemaVersion: 1, Direction: wiretrace.DirectionTx, Hex: "c635020001020003", Proto: "acp2", Transport: "an2"},
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: "c635020101020003", Note: "reply_v1"},
	}
	var buf bytes.Buffer
	if err := wiretrace.WriteTrames(&buf, in); err != nil {
		t.Fatalf("WriteTrames: %v", err)
	}
	out, err := wiretrace.ReadTrames(&buf)
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("round-trip length: got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i].Hex != in[i].Hex || out[i].Direction != in[i].Direction || out[i].Note != in[i].Note {
			t.Errorf("trame %d round-trip mismatch:\n  in  %+v\n  out %+v", i, in[i], out[i])
		}
	}
}
