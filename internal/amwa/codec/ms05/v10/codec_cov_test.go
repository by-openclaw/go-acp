package v10

import (
	"testing"

	"dhs/internal/amwa/codec/ms05"
)

// MS-05-02 has one wire minor; what the codec owes the version
// machinery is its identity, and that it registered itself under it.
func TestV10CodecIdentity(t *testing.T) {
	c := New()
	if c.SpecID() != ms05.SpecID || c.APIVer() != "v1.0" || c.SpecPatch() != SpecPatch {
		t.Errorf("identity = %q %q %q", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	got, ok := ms05.Get("v1.0")
	if !ok || got.APIVer() != "v1.0" {
		t.Errorf("the codec must register itself: %v, %v", got, ok)
	}
}
