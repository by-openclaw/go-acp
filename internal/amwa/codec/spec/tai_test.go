package spec

import (
	"testing"
	"time"
)

// TAI is one implementation for the whole suite (tai.go header): an
// activation stamped by IS-05 and an event stamped by IS-07 must agree
// to the second, so the offset is pinned here against a known instant.
func TestFormatTAIAppliesLeapOffset(t *testing.T) {
	// 2020-01-01T00:00:00Z = 1577836800 Unix; TAI is 37 s ahead.
	at := time.Unix(1577836800, 500).UTC()
	if got, want := FormatTAI(at), "1577836837:500"; got != want {
		t.Fatalf("FormatTAI = %q, want %q", got, want)
	}
}

func TestTAIToTimeIsInverseOfFormatTAI(t *testing.T) {
	at := time.Unix(1577836800, 123456789).UTC()
	sec, nsec, ok := ParseTAI(FormatTAI(at))
	if !ok {
		t.Fatalf("ParseTAI rejected FormatTAI output")
	}
	if got := TAIToTime(sec, nsec); !got.Equal(at) {
		t.Fatalf("round trip = %v, want %v", got, at)
	}
}

func TestParseTAI(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		sec, nsec int64
		ok        bool
	}{
		{"canonical", "1577836837:500", 1577836837, 500, true},
		{"zero", "0:0", 0, 0, true},
		{"no colon", "1577836837", 0, 0, false},
		{"non-numeric seconds", "abc:500", 0, 0, false},
		{"non-numeric nanoseconds", "1577836837:xyz", 0, 0, false},
		{"empty", "", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sec, nsec, ok := ParseTAI(tc.in)
			if ok != tc.ok || sec != tc.sec || nsec != tc.nsec {
				t.Fatalf("ParseTAI(%q) = (%d,%d,%v), want (%d,%d,%v)",
					tc.in, sec, nsec, ok, tc.sec, tc.nsec, tc.ok)
			}
		})
	}
}
