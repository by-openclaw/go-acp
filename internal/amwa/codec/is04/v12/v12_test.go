package v12

import (
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/spec"
	"encoding/json"
	"strings"
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

// TestNodeEncodeStripsAttachedNetworkDevice verifies the v1.2 wire
// drops `interfaces[].attached_network_device` (added v1.3) — closes #191.
func TestNodeEncodeStripsAttachedNetworkDevice(t *testing.T) {
	c := Codec{}
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	chassis := "ff-ff-ff-ff-ff-ff"
	n.Interfaces = []is04.NodeIface{
		{
			Name:      "eth0",
			ChassisID: &chassis,
			PortID:    "00-11-22-33-44-66",
			AttachedNetworkDevice: &is04.AttachedNetworkDevice{
				ChassisID: "00-11-22-33-44-55",
				PortID:    "Eth1/1",
			},
		},
	}
	body, err := c.EncodeNode(n)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(body), "attached_network_device") {
		t.Fatalf("v1.2 wire must not carry interfaces[].attached_network_device: %s", body)
	}
	// Sanity: the original Node struct must NOT be mutated by the encoder.
	if n.Interfaces[0].AttachedNetworkDevice == nil {
		t.Fatalf("EncodeNode mutated the caller's Node — interfaces[0].attached_network_device was nilled")
	}
}

// TestNodeDecodeRejectsAttachedNetworkDevice verifies the v1.2 decoder
// rejects a Node whose interfaces[] carry attached_network_device.
func TestNodeDecodeAbsorbsAttachedNetworkDevice(t *testing.T) {
	body := []byte(`{
	  "id":"f47ac10b-58cc-4372-a567-0e02b2c3d479",
	  "version":"1700000000:0","label":"x","description":"x","tags":{},
	  "href":"http://h/","caps":{},
	  "api":{"versions":["v1.2"],"endpoints":[{"host":"h","port":80,"protocol":"http"}]},
	  "services":[],"clocks":[],
	  "interfaces":[{"name":"eth0","chassis_id":"00-11-22-33-44-55","port_id":"00-11-22-33-44-66","attached_network_device":{"chassis_id":"x","port_id":"y"}}]
	}`)
	rep := &spec.SliceReporter{}
	if _, err := (Codec{Reporter: rep}).DecodeNode(body); err != nil {
		t.Fatalf("a later-minor field must be absorbed, not rejected: %v", err)
	}
	// AMWA's v1.2 schema does NOT forbid this property — there is no
	// additionalProperties:false on these resources. So the correct
	// behaviour is to accept it in silence. Our old hand-written rule
	// rejected it, and that rule was invented, not specified.
	if n := len(rep.Snapshot()); n != 0 {
		t.Fatalf("AMWA permits this on a v1.2 tree; %d deviations reported: %v",
			n, rep.Snapshot())
	}
}

// TestReceiverEncodeStripsBCPv13Caps verifies the v1.2 wire drops
// `caps.constraint_sets` and `caps.version` (BCP-004-01, added v1.3).
func TestReceiverEncodeStripsBCPv13Caps(t *testing.T) {
	c := Codec{}
	r := is04.Receiver{
		ResourceCore: is04.ResourceCore{
			ID:          "11111111-1111-4111-8111-111111111111",
			Version:     "1700000000:0",
			Label:       "v1.2 Receiver",
			Description: "fixture",
			Tags:        map[string][]string{},
		},
		Format:            "urn:x-nmos:format:video",
		DeviceID:          "abcdef01-1234-4abc-9def-1234567890ab",
		Transport:         "urn:x-nmos:transport:rtp",
		InterfaceBindings: []string{"eth0"},
		Subscription:      is04.ReceiverSubscription{Active: false},
		Caps: is04.ReceiverCaps{
			MediaTypes:     []string{"video/raw"},
			ConstraintSets: []map[string]any{{"urn:x-nmos:cap:format:color_sampling": map[string]any{"enum": []string{"YCbCr-4:2:2"}}}},
			Version:        "1700000000:0",
		},
	}
	body, err := c.EncodeReceiver(r)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	caps, ok := m["caps"].(map[string]any)
	if !ok {
		t.Fatalf("caps missing or wrong type: %v", m["caps"])
	}
	if _, present := caps["constraint_sets"]; present {
		t.Fatalf("v1.2 wire must not carry receiver.caps.constraint_sets: %s", body)
	}
	if _, present := caps["version"]; present {
		t.Fatalf("v1.2 wire must not carry receiver.caps.version: %s", body)
	}
	// Sanity: caller's Receiver must not be mutated.
	if len(r.Caps.ConstraintSets) == 0 || r.Caps.Version == "" {
		t.Fatalf("EncodeReceiver mutated the caller's Receiver: %+v", r.Caps)
	}
}

// TestReceiverDecodeAbsorbsBCPv13Caps verifies the v1.2 decoder KEEPS
// a Receiver whose caps carries constraint_sets or version — both
// arrived in v1.3 — and reports each as a deviation instead of
// refusing the resource.
func TestReceiverDecodeAbsorbsBCPv13Caps(t *testing.T) {
	cases := map[string]string{
		"constraint_sets": `{"id":"11111111-1111-4111-8111-111111111111","version":"1700000000:0","label":"x","description":"x","tags":{},"format":"urn:x-nmos:format:video","caps":{"constraint_sets":[]},"device_id":"abcdef01-1234-4abc-9def-1234567890ab","transport":"urn:x-nmos:transport:rtp","subscription":{"sender_id":null,"active":false},"interface_bindings":[]}`,
		"version":         `{"id":"11111111-1111-4111-8111-111111111111","version":"1700000000:0","label":"x","description":"x","tags":{},"format":"urn:x-nmos:format:video","caps":{"version":"1700000000:0"},"device_id":"abcdef01-1234-4abc-9def-1234567890ab","transport":"urn:x-nmos:transport:rtp","subscription":{"sender_id":null,"active":false},"interface_bindings":[]}`,
	}
	for name, body := range cases {
		rep := &spec.SliceReporter{}
		if _, err := (Codec{Reporter: rep}).DecodeReceiver([]byte(body)); err != nil {
			t.Fatalf("caps.%s must be absorbed, not rejected: %v", name, err)
		}
		// AMWA's v1.2 schema does NOT forbid this property — there is no
		// additionalProperties:false on these resources. So the correct
		// behaviour is to accept it in silence. Our old hand-written rule
		// rejected it, and that rule was invented, not specified.
		if n := len(rep.Snapshot()); n != 0 {
			t.Fatalf("AMWA permits this on a v1.2 tree; %d deviations reported: %v",
				n, rep.Snapshot())
		}
	}
}

// TestSchemaDeviationIsReportedNotFatal: a payload AMWA's own schema
// rejects is still returned to the caller — refusing it costs the
// operator the resource — but every failure is named as a compliance
// event so nothing is swallowed.
func TestSchemaDeviationIsReportedNotFatal(t *testing.T) {
	// `id` is not a UUID and `version` is not <secs>:<nanos>: two rules
	// AMWA states explicitly, so two deviations.
	bad := []byte(`{"id":"not-a-uuid","version":"whenever","label":"x","description":"","tags":{},"node_id":"22222222-2222-4222-8222-222222222222","type":"urn:x-nmos:device:generic","senders":[],"receivers":[]}`)
	rep := &spec.SliceReporter{}
	d, err := (Codec{Reporter: rep}).DecodeDevice(bad)
	if err != nil {
		t.Fatalf("a schema deviation must not stop the decode: %v", err)
	}
	if d.ID != "not-a-uuid" {
		t.Fatalf("the resource must still reach the caller, got %+v", d)
	}
	events := rep.Snapshot()
	if len(events) == 0 {
		t.Fatal("a schema deviation must be reported, not swallowed")
	}
	for _, e := range events {
		if e.Code != "nmos_is04_schema_deviation" {
			t.Errorf("code = %q", e.Code)
		}
		if e.APIVer != "v1.2" {
			t.Errorf("apiVer = %q", e.APIVer)
		}
	}
}
