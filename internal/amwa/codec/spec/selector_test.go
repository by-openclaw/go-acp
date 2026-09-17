package spec

import (
	"strings"
	"testing"
)

func TestErrNoCommonVersionNamesBothSides(t *testing.T) {
	err := ErrNoCommonVersion{
		SpecID:         "is-04",
		Mine:           []string{"v1.2", "v1.3"},
		PeerAdvertised: []string{"v2.0"},
	}
	msg := err.Error()
	for _, want := range []string{"is-04", "v1.2", "v1.3", "v2.0", "no mutually-supported version"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() = %q, missing %q", msg, want)
		}
	}
}

// Every ordering branch of the major/minor comparison, plus the two
// malformed shapes (missing minor; non-numeric) that fall back to a
// lexicographic compare so a bad peer string never panics.
func TestCompareAPIVerAllBranches(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		sign int
	}{
		{"major lower", "v1.9", "v2.0", -1},
		{"major higher", "v2.0", "v1.9", 1},
		{"minor lower", "v1.0", "v1.1", -1},
		{"minor higher", "v1.1", "v1.0", 1},
		{"equal", "v1.3", "v1.3", 0},
		{"missing minor falls back", "v1", "v1.0", -1},
		{"non-numeric falls back", "v1.x", "v1.0", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compareAPIVer(tc.a, tc.b)
			if (got < 0) != (tc.sign < 0) || (got > 0) != (tc.sign > 0) {
				t.Fatalf("compareAPIVer(%q,%q) = %d, want sign %d", tc.a, tc.b, got, tc.sign)
			}
		})
	}
}
