package acp2

import "testing"

// TestWireLabel pins the emulation-fidelity contract: labels are served
// VERBATIM (pid=2), even when they violate the spec charset of
// acp2_protocol.docx §"Versions" line 357 — the real EVS Neuron emits
// "ROOT_NODE_V2" with an underscore, and controllers (Cerebrum's Neuron
// driver, live-proven 2026-08-20) bind their default object model by
// exact label paths. The 2026-05-08 rewrite-to-charset behaviour
// orphaned the whole tree behind a renamed root. The deviation is
// surfaced via labelDeviatesSpec + the newTree counter instead of a
// silent mutation. Empty labels still default to "obj" (spec mandates
// non-empty).
func TestWireLabel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		out  string
	}{
		{"compliant", "User Label 1", "User Label 1"},
		{"underscore-verbatim", "ROOT_NODE_V2", "ROOT_NODE_V2"},
		{"dot-verbatim", "Sample.Rate", "Sample.Rate"},
		{"non-ascii-verbatim", "Café", "Café"},
		{"empty", "", "obj"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wireLabel(c.in); got != c.out {
				t.Errorf("wireLabel(%q) = %q; want %q", c.in, got, c.out)
			}
		})
	}
}

// TestLabelDeviatesSpec pins the deviation predicate used for the
// absorb-and-surface count.
func TestLabelDeviatesSpec(t *testing.T) {
	if labelDeviatesSpec("PSU - NPU0500-B") {
		t.Error("compliant label flagged as deviating")
	}
	if !labelDeviatesSpec("ROOT_NODE_V2") {
		t.Error("underscore label not flagged")
	}
}

// TestIsSpecLabelByte locks the per-rune predicate against the spec
// charset definition. Useful for catching accidental drift if the
// helper grows additional accepted characters.
func TestIsSpecLabelByte(t *testing.T) {
	allowed := []rune{
		'a', 'z', 'A', 'Z', '0', '9', ' ', '-',
	}
	disallowed := []rune{
		'_', '.', '/', '\\', '=', ':', ';', ',', '@', '#', '$',
		'(', ')', '[', ']', '{', '}', '<', '>',
		'\n', '\t', 0x00, 0xC2, // non-ASCII byte boundary
	}
	for _, r := range allowed {
		if !isSpecLabelByte(r) {
			t.Errorf("isSpecLabelByte(%q) = false; want true (spec §Versions allows it)", r)
		}
	}
	for _, r := range disallowed {
		if isSpecLabelByte(r) {
			t.Errorf("isSpecLabelByte(%q) = true; want false (spec §Versions forbids it)", r)
		}
	}
}
