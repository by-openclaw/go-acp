package v10

import (
	"testing"

	"acp/internal/amwa/codec/is12"
)

func TestRegistration(t *testing.T) {
	c, ok := is12.Get("v1.0")
	if !ok {
		t.Fatalf("is12.Get(v1.0) not found")
	}
	if c.SpecID() != is12.SpecID {
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
	got, err := is12.SelectHighest([]string{"v0.9", "v1.0"})
	if err != nil {
		t.Fatalf("SelectHighest: %v", err)
	}
	if got.APIVer() != "v1.0" {
		t.Fatalf("got %s", got.APIVer())
	}
}

func TestRoundTripViaCodec(t *testing.T) {
	c := New()
	in := is12.SubscriptionMessage{Subscriptions: []int{1, 2}}
	body, err := c.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := c.Decode(body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
}
