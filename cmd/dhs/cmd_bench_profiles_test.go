package main

import (
	"errors"
	"testing"

	"dhs/internal/errcode"
)

// TestResolveBenchProfile_Known covers every named profile.
func TestResolveBenchProfile_Known(t *testing.T) {
	cases := []struct {
		name string
		n    int
		op   string
	}{
		{"rfc2544-throughput", 10000, "connect"},
		{"rfc2544-latency", 1000, "absolute"},
		{"rfc2544-recovery", 500, "disconnect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, op, err := resolveBenchProfile(tc.name)
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if n != tc.n || op != tc.op {
				t.Errorf("got (n=%d, op=%q); want (n=%d, op=%q)", n, op, tc.n, tc.op)
			}
		})
	}
}

// TestResolveBenchProfile_Unknown asserts the validation:bench-
// profile-unknown sentinel chains correctly for CLI exit 2.
func TestResolveBenchProfile_Unknown(t *testing.T) {
	_, _, err := resolveBenchProfile("not-a-profile")
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !errors.Is(err, errBenchProfileUnknown) {
		t.Errorf("error chain missing errBenchProfileUnknown: %v", err)
	}
	c := errcode.From(err)
	if c == nil || c.Layer != errcode.LayerValidation ||
		c.Name != "bench-profile-unknown" || c.Class != errcode.ClassUsage {
		t.Errorf("typed code = %+v; want validation:bench-profile-unknown class=usage", c)
	}
}
