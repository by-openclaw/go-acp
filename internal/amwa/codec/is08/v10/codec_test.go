package v10

import (
	"testing"

	"dhs/internal/amwa/codec/is08"
)

func TestRegistration(t *testing.T) {
	c, ok := is08.Get("v1.0")
	if !ok {
		t.Fatalf("is08.Get(v1.0) not found — init() did not register?")
	}
	if c.SpecID() != is08.SpecID {
		t.Fatalf("spec id %q", c.SpecID())
	}
	if c.APIVer() != "v1.0" {
		t.Fatalf("api ver %q", c.APIVer())
	}
	if c.SpecPatch() != SpecPatch {
		t.Fatalf("patch %q", c.SpecPatch())
	}
}

func TestSelectHighest(t *testing.T) {
	got, err := is08.SelectHighest([]string{"v0.9", "v1.0", "v2.0"})
	if err != nil {
		t.Fatalf("SelectHighest: %v", err)
	}
	if got.APIVer() != "v1.0" {
		t.Fatalf("got %s", got.APIVer())
	}
}

func TestRoundTripViaCodec(t *testing.T) {
	c := New()
	in := is08.MapActivationRequest{
		Activation: is08.Activation{Mode: is08.ActivationModeImmediate},
		Action:     is08.MapEntries{},
	}
	body, err := c.EncodeMapActivationRequest(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := c.DecodeMapActivationRequest(body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
}
