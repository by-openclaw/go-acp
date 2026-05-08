package main

import (
	"strings"
	"testing"
)

// TestCanonicalDirection covers the alias logic per #324:
//   - "consumer" / "provider" / "both" → identity (canonical forms)
//   - "io" → "both" (friendlier synonym for bidirectional products)
//   - anything else → error citing the allowed values.
func TestCanonicalDirection(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		wantErr  bool
	}{
		{"consumer", "consumer", false},
		{"provider", "provider", false},
		{"both", "both", false},
		{"io", "both", false},
		{"sideways", "", true},
		{"", "", true},
		{"IO", "", true}, // case-sensitive — "IO" is not "io"
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := canonicalDirection(c.in)
			if c.wantErr {
				if err == nil {
					t.Errorf("canonicalDirection(%q) = %q, want error", c.in, got)
				}
				if err != nil && !strings.Contains(err.Error(), "--direction must be one of") {
					t.Errorf("canonicalDirection(%q) error = %v; want validator message", c.in, err)
				}
				return
			}
			if err != nil {
				t.Errorf("canonicalDirection(%q) error = %v; want nil", c.in, err)
			}
			if got != c.want {
				t.Errorf("canonicalDirection(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}
