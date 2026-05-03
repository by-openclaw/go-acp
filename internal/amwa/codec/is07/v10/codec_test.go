package v10

import (
	"testing"

	"dhs/internal/amwa/codec/is07"
)

func TestRegistration(t *testing.T) {
	c, ok := is07.Get("v1.0")
	if !ok {
		t.Fatalf("is07.Get(v1.0) not found — init() did not register?")
	}
	if c.SpecID() != is07.SpecID {
		t.Fatalf("spec id %q, want %q", c.SpecID(), is07.SpecID)
	}
	if c.APIVer() != "v1.0" {
		t.Fatalf("api ver %q, want v1.0", c.APIVer())
	}
	if c.SpecPatch() != SpecPatch {
		t.Fatalf("patch %q, want %q", c.SpecPatch(), SpecPatch)
	}
}

func TestRoundTripViaCodec(t *testing.T) {
	c := New()
	in := is07.MessageHealth{
		Timing: is07.Timing{
			CreationTimestamp: "10:20",
			OriginTimestamp:   "9:0",
		},
	}
	body, err := c.EncodeMessage(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := c.DecodeMessage(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := out.(is07.MessageHealth); !ok {
		t.Fatalf("decoded into %T, want MessageHealth", out)
	}
}

func TestSelectHighest(t *testing.T) {
	got, err := is07.SelectHighest([]string{"v0.9", "v1.0", "v2.0"})
	if err != nil {
		t.Fatalf("SelectHighest: %v", err)
	}
	if got.APIVer() != "v1.0" {
		t.Fatalf("expected v1.0 (only registered minor), got %s", got.APIVer())
	}
}

func TestSupportedVersions(t *testing.T) {
	versions := is07.SupportedVersions()
	if len(versions) == 0 {
		t.Fatalf("no versions registered")
	}
	want := "v1.0"
	for _, v := range versions {
		if v == want {
			return
		}
	}
	t.Fatalf("v1.0 missing from %v", versions)
}
