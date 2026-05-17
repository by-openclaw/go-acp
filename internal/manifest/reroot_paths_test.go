package manifest

import (
	"testing"

	"dhs/internal/export/canonical"
)

// TestRerootPaths_StrictCanonical pins #456: when a slot DM is grafted
// under a synthetic root whose identifier differs from the slot's
// Root.Path first segment, every descendant's Path must be rewritten
// to begin with the synthetic root.
//
// This is the case for the spec-clean integration-test DMs (per the
// scope refresh in #464): identity-strict / oneToN-strict / etc. all
// carry bare paths ("oneToN.X.Y") because no single shared prefix
// exists across the slot Roots. Without rerooting, the provider's
// byIDP index in internal/emberplus/provider/tree.go ends up keyed
// by the un-prefixed paths and the dotted form a consumer naturally
// assembles ("<device>.oneToN.X.Y") fails to resolve.
func TestRerootPaths_StrictCanonical(t *testing.T) {
	leaf := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gain", Path: "oneToN.matrix.gain", OID: "1.1.3.1",
		},
		Type: "integer",
	}
	mid := &canonical.Node{
		Header: canonical.Header{
			Number: 3, Identifier: "matrix", Path: "oneToN.matrix", OID: "1.1.3",
			Children: []canonical.Element{leaf},
		},
	}
	slot := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "oneToN", Path: "oneToN", OID: "1.1",
			Children: []canonical.Element{mid},
		},
	}

	rerootPaths(slot, "dhs-emberplus-integration")

	if got, want := slot.Path, "dhs-emberplus-integration.oneToN"; got != want {
		t.Errorf("slot Path = %q, want %q", got, want)
	}
	if got, want := mid.Path, "dhs-emberplus-integration.oneToN.matrix"; got != want {
		t.Errorf("mid Path = %q, want %q", got, want)
	}
	if got, want := leaf.Path, "dhs-emberplus-integration.oneToN.matrix.gain"; got != want {
		t.Errorf("leaf Path = %q, want %q", got, want)
	}
}

// TestRerootPaths_Idempotent confirms a second call with the same
// prefix is a no-op — needed because a re-run gen script + producer
// restart could legitimately call rerootPaths twice on the same
// in-RAM tree if a refactor ever caches it.
func TestRerootPaths_Idempotent(t *testing.T) {
	n := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "x", Path: "x", OID: "1",
			Children: []canonical.Element{
				&canonical.Parameter{Header: canonical.Header{Identifier: "y", Path: "x.y", OID: "1.1"}, Type: "string"},
			},
		},
	}
	rerootPaths(n, "dev")
	first := n.Path
	rerootPaths(n, "dev")
	if first != n.Path {
		t.Errorf("rerootPaths not idempotent: 1st=%q 2nd=%q", first, n.Path)
	}
	if got, want := n.Path, "dev.x"; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
	// Leaf still consistent
	leaf := n.Header.Children[0].Common()
	if got, want := leaf.Path, "dev.x.y"; got != want {
		t.Errorf("leaf Path = %q, want %q", got, want)
	}
}

// TestRerootPaths_AlreadyPrefixed leaves paths alone when the slot DM
// already carries the device-prefixed form (e.g. the legacy Tiny
// Ember+ Router 1.6.2 DMs whose slot Root.Path starts with "router"
// when the synthetic root is also "router").
func TestRerootPaths_AlreadyPrefixed(t *testing.T) {
	n := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "router", Path: "router", OID: "1",
			Children: []canonical.Element{
				&canonical.Parameter{Header: canonical.Header{Identifier: "x", Path: "router.x", OID: "1.1"}, Type: "string"},
			},
		},
	}
	rerootPaths(n, "router")
	if got, want := n.Path, "router"; got != want {
		t.Errorf("Path = %q, want %q (no double-prefix)", got, want)
	}
	leaf := n.Header.Children[0].Common()
	if got, want := leaf.Path, "router.x"; got != want {
		t.Errorf("leaf Path = %q, want %q (no double-prefix)", got, want)
	}
}

// TestRerootPaths_EmptyPrefixIsNoOp guards against accidental
// rerooting when BuildExport's rootIdent computation degrades to "".
func TestRerootPaths_EmptyPrefixIsNoOp(t *testing.T) {
	n := &canonical.Node{
		Header: canonical.Header{Identifier: "x", Path: "x", OID: "1"},
	}
	rerootPaths(n, "")
	if got, want := n.Path, "x"; got != want {
		t.Errorf("Path = %q, want %q (empty prefix should not mutate)", got, want)
	}
}
