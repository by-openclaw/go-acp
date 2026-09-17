package main

import (
	"errors"
	"testing"

	"dhs/internal/consumer"
)

// TestValidatePathOrOID_AcceptedForms pins the R21 #486 spec table:
// every legitimate path shape — numeric OID, dotted label, single-
// segment numeric or label — passes the validator. Tree-miss is
// orthogonal (plugin:object-not-found, surfaced by the resolver).
func TestValidatePathOrOID_AcceptedForms(t *testing.T) {
	cases := []string{
		"1.6.1",                                 // OID
		"1",                                     // root OID
		"1.0.4",                                 // identity.dtdVersion
		"1.6.10",                                // stream parameter
		"identity.types.vInteger",               // dotted label
		"router.oneToN.matrix",                  // multi-segment label
		"vInteger",                              // single-segment label
		"label-with-hyphen",                     // label with hyphen
		"dhs-emberplus-integration.types.vEnum", // hyphen + dot
		"label_with_underscore",                 // label with underscore
		"1.label.2",                             // mixed digit/label/digit
	}
	for _, in := range cases {
		if err := validatePathOrOID(in); err != nil {
			t.Errorf("validatePathOrOID(%q) = %v, want nil", in, err)
		}
	}
}

// TestValidatePathOrOID_InvalidOID pins the validation:invalid-oid
// surface: inputs composed of digits and dots only (i.e. they failed
// the dotted-label fallback in the resolver) must hit ErrInvalidOID
// when their OID syntax is malformed.
func TestValidatePathOrOID_InvalidOID(t *testing.T) {
	cases := []string{
		"1..2", // double dot
		"1.",   // trailing dot
		".1",   // leading dot
		"1.0.", // trailing dot multi-segment
		".",    // dot only
		"..",   // two dots
	}
	for _, in := range cases {
		err := validatePathOrOID(in)
		if err == nil {
			t.Errorf("validatePathOrOID(%q) returned nil, want ErrInvalidOID", in)
			continue
		}
		if !errors.Is(err, consumer.ErrInvalidOID) {
			t.Errorf("validatePathOrOID(%q) = %v, want errors.Is ErrInvalidOID", in, err)
		}
	}
}

// TestValidatePathOrOID_EmptyPath pins the empty-string rejection. The
// caller wraps this in the verb's usage-error path; we never silently
// pass an empty string to the resolver (which would short-circuit to a
// stale ID-only path).
func TestValidatePathOrOID_EmptyPath(t *testing.T) {
	err := validatePathOrOID("")
	if err == nil {
		t.Fatal("empty path accepted, want error")
	}
	if errors.Is(err, consumer.ErrInvalidOID) {
		t.Errorf("empty path returned ErrInvalidOID; should be a plain usage error")
	}
}

// TestLooksLikeNumericOID pins the OID-vs-label gate the validator uses
// to decide which branch to take. Any non-digit/non-dot rune flips the
// gate to "label", so a label like `1.a.3` is never wrongly classified
// as a malformed OID.
func TestLooksLikeNumericOID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"1.6.1", true},
		{"1", true},
		{"1.0.4", true},
		{"1..2", true}, // looks numeric — caught by syntax check
		{"1.", true},
		{".1", true},
		{"", false},
		{"1.a", false},
		{"identity", false},
		{"router.oneToN", false},
		{"1.label.2", false},
		{" 1.2", false},
	}
	for _, c := range cases {
		if got := looksLikeNumericOID(c.in); got != c.want {
			t.Errorf("looksLikeNumericOID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
