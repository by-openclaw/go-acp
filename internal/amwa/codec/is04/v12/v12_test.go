package v12

import (
	"acp/internal/amwa/codec/is04"
	"acp/internal/amwa/codec/spec"
	"testing"
)

func TestCodecVersionedShape(t *testing.T) {
	c := Codec{}
	if c.SpecID() != "is-04" {
		t.Fatalf("SpecID = %q", c.SpecID())
	}
	if c.APIVer() != "v1.2" {
		t.Fatalf("APIVer = %q", c.APIVer())
	}
	if c.SpecPatch() != "v1.2.2" {
		t.Fatalf("SpecPatch = %q", c.SpecPatch())
	}
}

func TestCodecRegistersOnInit(t *testing.T) {
	got, ok := is04.Get("v1.2")
	if !ok {
		t.Fatalf("is04.Get(v1.2) miss — init() did not register")
	}
	if got.APIVer() != "v1.2" {
		t.Fatalf("registered APIVer = %q", got.APIVer())
	}
}

func TestCodecImplementsInterface(t *testing.T) {
	var _ is04.Codec = Codec{}
	var _ spec.Versioned = Codec{}
}

// validNode mirrors v13_test.go validNode but without v1.3-only
// fields. v1.2 still requires interfaces (added in v1.2), so we keep
// the slice populated.
func validNode(id string) is04.Node {
	chassis := "00-11-22-33-44-55"
	return is04.Node{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "v1.2 Node",
			Description: "fixture",
			Tags:        map[string][]string{},
		},
		Hostname: "dhs.local",
		Href:     "http://dhs.local:8080/",
		Caps:     map[string]any{},
		API: is04.NodeAPI{
			Versions:  []string{"v1.2"},
			Endpoints: []is04.NodeEndpoint{{Host: "dhs.local", Port: 8080, Protocol: "http"}},
		},
		Services:   []is04.NodeService{},
		Clocks:     []is04.NodeClock{},
		Interfaces: []is04.NodeIface{{Name: "eth0", ChassisID: &chassis, PortID: "00-11-22-33-44-66"}},
	}
}

func TestNodeRoundTrip(t *testing.T) {
	c := Codec{}
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	body, err := c.EncodeNode(n)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.DecodeNode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != n.ID {
		t.Fatalf("round-trip mismatch")
	}
}
