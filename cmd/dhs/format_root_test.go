package main

import "testing"

func TestStripDisplayRoot(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// acp2 device root stripped to match Cerebrum's UI + --path form.
		{"ROOT_NODE_V2.REFERENCE.LOCK CIRCUIT 1.PTP Time", "REFERENCE.LOCK CIRCUIT 1.PTP Time"},
		{"ROOT_NODE_V2.OUTPUT", "OUTPUT"},
		// Case-insensitive, matching the export root-strip.
		{"root_node_v2.INPUT", "INPUT"},
		// acp1 root.
		{"ROOT.BOARD.Gain", "BOARD.Gain"},
		// Bare root stays visible (nothing to strip below it).
		{"ROOT_NODE_V2", "ROOT_NODE_V2"},
		// Non-root prefixes pass through untouched (Ember+, probel).
		{"INPUT.SDI.CHANNEL 01", "INPUT.SDI.CHANNEL 01"},
		{"Tiny Ember+ Router.identity.product", "Tiny Ember+ Router.identity.product"},
		{"", ""},
	}
	for _, c := range cases {
		if got := stripDisplayRoot(c.in); got != c.want {
			t.Errorf("stripDisplayRoot(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsDisplayRoot(t *testing.T) {
	for _, r := range []string{"ROOT_NODE_V2", "root_node_v2", "ROOT", "root"} {
		if !isDisplayRoot(r) {
			t.Errorf("isDisplayRoot(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"INPUT", "OUTPUT", "ROOTish", "", "REFERENCE"} {
		if isDisplayRoot(r) {
			t.Errorf("isDisplayRoot(%q) = true, want false", r)
		}
	}
}
