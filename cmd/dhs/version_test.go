package main

import (
	"strings"
	"testing"
)

// TestIdentityConstants asserts the binary carries vendor/product provenance —
// the cross-platform identity surfaced by `dhs version` (and mirrored into the
// Windows .exe resource via cmd/dhs/winres/winres.json).
func TestIdentityConstants(t *testing.T) {
	cases := map[string]string{
		"productName":   productName,
		"vendorName":    vendorName,
		"vendorURL":     vendorURL,
		"repositoryURL": repositoryURL,
		"supportURL":    supportURL,
		"copyrightLine": copyrightLine,
		"licenseName":   licenseName,
	}
	for name, v := range cases {
		if strings.TrimSpace(v) == "" {
			t.Errorf("identity constant %s is empty", name)
		}
	}
	if !strings.Contains(vendorName, "BY-SYSTEMS") {
		t.Errorf("vendorName = %q, want it to name BY-SYSTEMS", vendorName)
	}
	if !strings.HasPrefix(vendorURL, "https://") || !strings.HasPrefix(repositoryURL, "https://") {
		t.Errorf("vendor/repo URLs must be https: %q / %q", vendorURL, repositoryURL)
	}
}

func TestOrUnknown(t *testing.T) {
	if orUnknown("") != "unknown" {
		t.Error("empty should map to unknown")
	}
	if orUnknown("abc123") != "abc123" {
		t.Error("non-empty should pass through")
	}
}
