package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dhs/internal/errcode"
)

// TestParseEnsureMode covers every accepted value + the rejection path.
func TestParseEnsureMode(t *testing.T) {
	cases := []struct {
		in   string
		want ensureMode
		err  bool
	}{
		{"", ensureUnset, false},
		{"present", ensurePresent, false},
		{"absent", ensureAbsent, false},
		{"dryrun", ensureDryrun, false},
		{"yes", "", true},
		{"PRESENT", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseEnsureMode(tc.in)
			if tc.err {
				if err == nil {
					t.Errorf("want error; got %q", got)
				} else if !errors.Is(err, errEnsureInvalidMode) {
					t.Errorf("error chain missing errEnsureInvalidMode: %v", err)
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

// TestEnsureReport_Schema verifies the JSON shape matches R14 spec.
func TestEnsureReport_Schema(t *testing.T) {
	r := ensureReport{
		Verb: "set", Ensure: "present",
		Changed: true, Before: "-25", After: "-30",
		Diff: "value: -25 -> -30",
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"verb":"set"`,
		`"ensure":"present"`,
		`"changed":true`,
		`"before":"-25"`,
		`"after":"-30"`,
		`"diff":"value: -25 -> -30"`,
	} {
		if !strings.Contains(string(buf), want) {
			t.Errorf("missing %q in: %s", want, buf)
		}
	}
}

// TestEnsureFmtDiff verifies the diff string format + empty-on-equal.
func TestEnsureFmtDiff(t *testing.T) {
	if got := ensureFmtDiff("value", -25, -30); got != "value: -25 -> -30" {
		t.Errorf("got %q; want value: -25 -> -30", got)
	}
	if got := ensureFmtDiff("value", "On", "On"); got != "" {
		t.Errorf("equal values diff = %q; want empty", got)
	}
}

// TestEnsureErrors_Codes asserts the R14 sentinels carry the typed
// validation:* / ClassUsage chain.
func TestEnsureErrors_Codes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		name string
	}{
		{errEnsureInvalidMode, "ensure-invalid-mode"},
		{errEnsureNoDefault, "no-default-declared"},
		{errEnsureModePending, "ensure-mode-pending"},
	} {
		c := errcode.From(tc.err)
		if c == nil || c.Layer != errcode.LayerValidation ||
			c.Name != tc.name || c.Class != errcode.ClassUsage {
			t.Errorf("err %v: got %+v; want validation:%s class=usage", tc.err, c, tc.name)
		}
	}
}
