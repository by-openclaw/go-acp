package registry

// The IS-04 version-mismatch matrix (#849), asserted as the registry's
// own decision function. The registry translates one way only: it can
// present a LOWER-registered resource at a HIGHER query, never a
// higher-registered resource to a lower controller — query.downgrade
// widens the window to [downgrade, urlVer] but cannot reach above
// urlVer. The operational consequence the customer plant showed:
// an old controller is permanently blind to new nodes, and the only
// lever is the minor the NODE registers at.

import "testing"

func TestVersionMismatchMatrix(t *testing.T) {
	cases := []struct {
		name                           string
		resourceVer, urlVer, downgrade string
		want                           bool
	}{
		// Node registered LOW, controller queries at/above — visible.
		{"v1.0 node at v1.0 query", "v1.0", "v1.0", "", true},
		{"v1.0 node at v1.3 query, no downgrade", "v1.0", "v1.3", "", false},
		{"v1.0 node at v1.3 query, downgrade v1.0", "v1.0", "v1.3", "v1.0", true},
		{"v1.1 node at v1.3 query, downgrade v1.0", "v1.1", "v1.3", "v1.0", true},

		// Node registered HIGH, controller queries LOW — the one-way
		// wall. downgrade cannot help: the window is [downgrade, urlVer]
		// and v1.3 > v1.0.
		{"v1.3 node at v1.0 query, no downgrade", "v1.3", "v1.0", "", false},
		{"v1.3 node at v1.0 query, downgrade v1.0", "v1.3", "v1.0", "v1.0", false},
		{"v1.3 node at v1.3 query", "v1.3", "v1.3", "", true},

		// downgrade floor excludes a resource below it.
		{"v1.0 node at v1.3 query, downgrade v1.2", "v1.0", "v1.3", "v1.2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionAllowed(tc.resourceVer, tc.urlVer, tc.downgrade); got != tc.want {
				t.Fatalf("versionAllowed(%q,%q,%q) = %v, want %v",
					tc.resourceVer, tc.urlVer, tc.downgrade, got, tc.want)
			}
		})
	}
}

// TestOldControllerBlindToNewNodes is the matrix's headline case as a
// standalone assertion: no query.downgrade value a v1.0 controller can
// send makes a v1.3-registered node visible to it.
func TestOldControllerBlindToNewNodes(t *testing.T) {
	for _, dg := range []string{"", "v1.0", "v1.1", "v1.2", "v1.3"} {
		if versionAllowed("v1.3", "v1.0", dg) {
			t.Fatalf("a v1.3 node became visible at a v1.0 query with downgrade=%q — the one-way wall is broken", dg)
		}
	}
}
