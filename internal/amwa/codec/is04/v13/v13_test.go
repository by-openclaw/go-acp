package v13

import (
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/spec"
	"strings"
	"testing"
)

// validNode constructs a minimum-valid v1.3.3 Node fixture for
// round-trip tests. Mirrors the fixture pattern used in the parent
// is04 tests.
func validNode(id string) is04.Node {
	chassis := "00-11-22-33-44-55"
	return is04.Node{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "dhs lab Node",
			Description: "v1.3 round-trip fixture",
			Tags:        map[string][]string{},
		},
		Hostname: "dhs-lab.local",
		Href:     "http://dhs-lab.local:8080/",
		Caps:     map[string]any{},
		API: is04.NodeAPI{
			Versions: []string{"v1.3"},
			Endpoints: []is04.NodeEndpoint{
				{Host: "dhs-lab.local", Port: 8080, Protocol: "http"},
			},
		},
		Services: []is04.NodeService{},
		Clocks:   []is04.NodeClock{},
		Interfaces: []is04.NodeIface{
			{Name: "eth0", ChassisID: &chassis, PortID: "00-11-22-33-44-66"},
		},
	}
}

func TestCodecVersionedShape(t *testing.T) {
	c := Codec{}
	if c.SpecID() != "is-04" {
		t.Fatalf("SpecID = %q", c.SpecID())
	}
	if c.APIVer() != "v1.3" {
		t.Fatalf("APIVer = %q", c.APIVer())
	}
	if c.SpecPatch() != "v1.3.3" {
		t.Fatalf("SpecPatch = %q", c.SpecPatch())
	}
}

func TestCodecRegistersOnInit(t *testing.T) {
	got, ok := is04.Get("v1.3")
	if !ok {
		t.Fatalf("is04.Get(v1.3) miss — init() did not register")
	}
	if got.APIVer() != "v1.3" {
		t.Fatalf("registered APIVer = %q", got.APIVer())
	}
}

func TestCodecImplementsInterface(t *testing.T) {
	// Compile-time check by assignment; runtime confirmation by
	// fetching from the spec registry.
	var _ is04.Codec = Codec{}
	var _ spec.Versioned = Codec{}
}

func TestNodeRoundTrip(t *testing.T) {
	c := Codec{}
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	body, err := c.EncodeNode(n)
	if err != nil {
		t.Fatalf("EncodeNode: %v", err)
	}
	got, err := c.DecodeNode(body)
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	if got.ID != n.ID || got.Label != n.Label {
		t.Fatalf("round-trip mismatch: %+v vs %+v", got, n)
	}
}

func TestNodeValidateRejectsBadUUID(t *testing.T) {
	c := Codec{}
	n := validNode("not-a-uuid")
	if err := c.ValidateNode(n); err == nil {
		t.Fatalf("expected validation error for bad UUID")
	}
}

// TestDecodeAbsorbsUnknownField: a future or vendor field does not
// stop the decode. See is04.TestNodeUnknownFieldAbsorbedAndReported
// for why this contract was inverted.
//
// The payload below is otherwise incomplete, so the decode still
// fails validation — the point here is only that it does NOT fail on
// `future_field_not_in_spec`.
func TestDecodeAbsorbsUnknownField(t *testing.T) {
	c := Codec{}
	body := []byte(`{"id":"f47ac10b-58cc-4372-a567-0e02b2c3d479",
	  "version":"1:0","label":"x","description":"x",
	  "hostname":"h","href":"http://h/","api":{"versions":["v1.3"]},
	  "future_field_not_in_spec":"oops"}`)
	_, err := c.DecodeNode(body)
	if err != nil && strings.Contains(err.Error(), "future_field_not_in_spec") {
		t.Fatalf("the unknown field must be absorbed, not rejected: %v", err)
	}
}
