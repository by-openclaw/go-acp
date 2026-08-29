package v12

import (
	"testing"

	"dhs/internal/amwa/codec/is05"
)

// TestRegisteredAsV12 pins that the minor reaches the registry. A
// codec that compiles but never registers is invisible: the provider
// serves the version trees it finds in is05.SupportedVersions(), so an
// unregistered minor is a /v1.2/ tree that silently does not exist.
func TestRegisteredAsV12(t *testing.T) {
	c, ok := is05.Get("v1.2")
	if !ok {
		t.Fatal("IS-05 v1.2 is not registered")
	}
	if c.SpecID() != is05.SpecID {
		t.Errorf("SpecID = %q, want %q", c.SpecID(), is05.SpecID)
	}
	if c.APIVer() != "v1.2" {
		t.Errorf("APIVer = %q, want v1.2", c.APIVer())
	}
	// The patch identifies WHICH revision of the v1.2 text this codec
	// was audited against. It never appears on the wire; it is the
	// claim we make about our reading.
	if c.SpecPatch() != "v1.2.0" {
		t.Errorf("SpecPatch = %q, want v1.2.0", c.SpecPatch())
	}
}

// TestV12SelectsItself: within this package only v12's init has run,
// so selection here can only prove that a v1.2-capable peer is met at
// v1.2. Whether v1.2 WINS over v1.0/v1.1 depends on every minor being
// registered, which is a property of the binary rather than of this
// package — asserted in cmd/dhs where all three are blank-imported.
func TestV12SelectsItself(t *testing.T) {
	c, err := is05.SelectHighest([]string{"v1.1", "v1.2"})
	if err != nil {
		t.Fatalf("SelectHighest: %v", err)
	}
	if c.APIVer() != "v1.2" {
		t.Errorf("selected %q against a v1.2-capable peer, want v1.2", c.APIVer())
	}
	// A peer with no version in common is an error, never a silent
	// downgrade.
	if _, err := is05.SelectHighest([]string{"v0.9"}); err == nil {
		t.Error("no common version must be an error, not a quiet fallback")
	}
}