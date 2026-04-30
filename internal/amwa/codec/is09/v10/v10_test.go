package v10

import (
	"acp/internal/amwa/codec/is09"
	"acp/internal/amwa/codec/spec"
	"testing"
)

func TestCodecVersionedShape(t *testing.T) {
	c := Codec{}
	if c.SpecID() != "is-09" {
		t.Fatalf("SpecID = %q", c.SpecID())
	}
	if c.APIVer() != "v1.0" {
		t.Fatalf("APIVer = %q", c.APIVer())
	}
	if c.SpecPatch() != "v1.0.0" {
		t.Fatalf("SpecPatch = %q", c.SpecPatch())
	}
}

func TestCodecRegistersOnInit(t *testing.T) {
	got, ok := is09.Get("v1.0")
	if !ok {
		t.Fatalf("is09.Get(v1.0) miss — init() did not register")
	}
	if got.APIVer() != "v1.0" {
		t.Fatalf("registered APIVer = %q", got.APIVer())
	}
}

func TestCodecImplementsInterface(t *testing.T) {
	var _ is09.Codec = Codec{}
	var _ spec.Versioned = Codec{}
}

func TestSupportedVersionsContainsV10(t *testing.T) {
	vs := is09.SupportedVersions()
	found := false
	for _, v := range vs {
		if v == "v1.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SupportedVersions = %v, expected v1.0", vs)
	}
}

// validGlobal mirrors the fixture pattern used in the parent
// is09 tests. Round-trip exercises EncodeGlobal / DecodeGlobal /
// ValidateGlobal through the Codec interface.
func validGlobal() is09.Global {
	return is09.Global{
		ID:          "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Version:     "1700000000:0",
		Label:       "v1.0 lab system",
		Description: "fixture",
		Tags:        map[string][]string{},
		IS04:        is09.IS04Config{HeartbeatInterval: 5},
		PTP: is09.PTPConfig{
			AnnounceReceiptTimeout: 3,
			DomainNumber:           127,
		},
	}
}

func TestGlobalRoundTrip(t *testing.T) {
	c := Codec{}
	g := validGlobal()
	body, err := c.EncodeGlobal(g)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.DecodeGlobal(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != g.ID {
		t.Fatalf("round-trip mismatch: %q != %q", got.ID, g.ID)
	}
}
