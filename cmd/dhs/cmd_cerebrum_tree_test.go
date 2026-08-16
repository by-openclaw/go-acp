package main

import (
	"testing"

	"dhs/internal/consumer"
)

// TestCerebrumExpandFocus pins the focus resolution against nested
// category paths: user segments match IN ORDER with gaps allowed
// (case-insensitive), expanding to the canonical path prefix ending at
// the last matched segment; shallowest match wins; no match passes the
// input through untouched.
func TestCerebrumExpandFocus(t *testing.T) {
	objs := []consumer.Object{
		{Path: []string{"Categories", "DESTINATIONS", "DST-FUSION", "0001 TEXT"}},
		{Path: []string{"Categories", "DESTINATIONS", "DST-CLIENTS", "DST-EBU", "0001 DEST"}},
		{Path: []string{"Salvos", "Salvo Group 1", "Salvo_001"}},
	}
	cases := []struct{ in, want string }{
		// gap between Categories and DST-FUSION (DESTINATIONS skipped)
		{"Categories.DST-FUSION", "Categories.DESTINATIONS.DST-FUSION"},
		// bare category name from any depth
		{"DST-FUSION", "Categories.DESTINATIONS.DST-FUSION"},
		{"DST-EBU", "Categories.DESTINATIONS.DST-CLIENTS.DST-EBU"},
		// full canonical path resolves to itself
		{"Categories.DESTINATIONS.DST-FUSION", "Categories.DESTINATIONS.DST-FUSION"},
		// case-insensitive
		{"categories.dst-clients", "Categories.DESTINATIONS.DST-CLIENTS"},
		// salvo side
		{"Salvo Group 1", "Salvos.Salvo Group 1"},
		// no match passes through
		{"Categories.NOPE", "Categories.NOPE"},
		// empty stays empty
		{"", ""},
	}
	for _, c := range cases {
		if got := cerebrumExpandFocus(objs, c.in); got != c.want {
			t.Errorf("expand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
