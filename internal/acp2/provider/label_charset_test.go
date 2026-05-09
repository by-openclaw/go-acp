package acp2

import "testing"

// TestSanitizeLabel pins the spec invariant from acp2_protocol.docx
// §"Versions" line 357: object labels MUST contain only characters
// from `[A-Za-z0-9 \-]`. Any other character is replaced with `-`
// before the label hits the wire (pid=2). The unmodified label stays
// on the canonical element so other tooling can see the original.
func TestSanitizeLabel(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		out   string
		dirty bool // whether the function should have changed the input
	}{
		// Compliant labels round-trip identity.
		{"all-lower", "abcdef", "abcdef", false},
		{"mixed-case-digits", "Card1234", "Card1234", false},
		{"with-space", "User Label 1", "User Label 1", false},
		{"with-dash", "Slot-A", "Slot-A", false},
		{"compound", "PSU - NPU0500-B", "PSU - NPU0500-B", false},
		{"channel-N", "CHANNEL 01", "CHANNEL 01", false},

		// Real-Neuron deviations — spec violations sanitised.
		{"underscore", "ROOT_NODE_V2", "ROOT-NODE-V2", true},
		{"dot", "Sample.Rate", "Sample-Rate", true},
		{"colon", "MAC:01:23", "MAC-01-23", true},
		{"slash", "A/B", "A-B", true},
		{"equal-sign", "Mode=Auto", "Mode-Auto", true},

		// Non-ASCII gets stripped.
		{"non-ascii", "Café", "Caf-", true},

		// Edge cases.
		{"empty", "", "obj", true},
		{"all-invalid", "___", "obj", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeLabel(c.in)
			if got != c.out {
				t.Errorf("sanitizeLabel(%q) = %q; want %q", c.in, got, c.out)
			}
		})
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
