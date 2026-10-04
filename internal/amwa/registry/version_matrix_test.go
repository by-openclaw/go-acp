package registry

// The IS-04 version-mismatch matrix (#849, corrected by #1337),
// asserted as the registry's own decision function.
//
// IS-04 v1.3.3, docs/Upgrade Path, "Requirements for Registries":
//
//   - a resource registered at a LATER minor than the query's is shown,
//     translated: "Query APIs MUST provide translations of resources
//     for backwards compatibility … by removing keys". An old
//     controller sees new nodes.
//   - a resource registered at an EARLIER minor is not shown unless the
//     client asks: "Query APIs do not need to provide for forwards
//     compatibility"; query.downgrade widens the window down to the
//     minor it names.
//
// The first rule was read the other way round here until the registry
// was paired with nmos-cpp, which does what the text says.

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

		// Node registered HIGH, controller queries LOW — shown, with
		// the keys the lower minor does not know removed. No downgrade
		// is asked for or needed.
		{"v1.3 node at v1.0 query, no downgrade", "v1.3", "v1.0", "", true},
		{"v1.3 node at v1.0 query, downgrade v1.0", "v1.3", "v1.0", "v1.0", true},
		{"v1.3 node at v1.2 query", "v1.3", "v1.2", "", true},
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

// TestOldControllerSeesNewNodes is the matrix's headline case as a
// standalone assertion: a v1.3-registered node is visible to a v1.0
// controller whatever query.downgrade it sends, or none.
func TestOldControllerSeesNewNodes(t *testing.T) {
	for _, dg := range []string{"", "v1.0", "v1.1", "v1.2", "v1.3"} {
		if !versionAllowed("v1.3", "v1.0", dg) {
			t.Fatalf("a v1.3 node is hidden from a v1.0 query with downgrade=%q — IS-04 Upgrade Path says it MUST be shown, translated", dg)
		}
	}
}
