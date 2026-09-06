package main

import (
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// TestNMOSVersionsRegisteredInTheBinary pins what the shipped binary
// actually serves, which is the only place the answer is true: each
// minor registers from its own init(), so a package-level test can
// only ever see its own.
//
// This is the assertion that would have caught IS-05 v1.2 being
// published and unimplemented (#861) — the codec existed in the tree
// for nobody if main.go never blank-imported it, and the provider
// serves the version trees it finds in SupportedVersions().
func TestNMOSVersionsRegisteredInTheBinary(t *testing.T) {
	for _, tc := range []struct {
		spec string
		got  []string
		want []string
	}{
		{"IS-04", is04.SupportedVersions(), []string{"v1.0", "v1.1", "v1.2", "v1.3"}},
		{"IS-05", is05.SupportedVersions(), []string{"v1.0", "v1.1", "v1.2"}},
	} {
		if len(tc.got) != len(tc.want) {
			t.Errorf("%s serves %v, spec publishes %v — a published minor missing here is a MISSING implementation, never a scope decision (internal/amwa/CLAUDE.md)",
				tc.spec, tc.got, tc.want)
			continue
		}
		for i := range tc.want {
			if tc.got[i] != tc.want[i] {
				t.Errorf("%s serves %v, want %v", tc.spec, tc.got, tc.want)
				break
			}
		}
	}

	// Highest-mutual selection across the whole registered set: a peer
	// offering everything must be met at the newest minor, and a peer
	// that stops earlier must be met there rather than refused.
	c, err := is05.SelectHighest([]string{"v1.0", "v1.1", "v1.2"})
	if err != nil || c.APIVer() != "v1.2" {
		t.Errorf("IS-05 against a v1.2 peer selected %v (err %v), want v1.2", c, err)
	}
	c, err = is05.SelectHighest([]string{"v1.0", "v1.1"})
	if err != nil || c.APIVer() != "v1.1" {
		t.Errorf("IS-05 against a v1.1-only peer selected %v (err %v), want v1.1", c, err)
	}
}
