package v10

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
	if c.APIVer() != "v1.0" {
		t.Fatalf("APIVer = %q", c.APIVer())
	}
	if c.SpecPatch() != "v1.0.3" {
		t.Fatalf("SpecPatch = %q", c.SpecPatch())
	}
}

func TestCodecRegistersOnInit(t *testing.T) {
	got, ok := is04.Get("v1.0")
	if !ok {
		t.Fatalf("is04.Get(v1.0) miss — init() did not register")
	}
	if got.APIVer() != "v1.0" {
		t.Fatalf("registered APIVer = %q", got.APIVer())
	}
}

func TestCodecImplementsInterface(t *testing.T) {
	var _ is04.Codec = Codec{}
	var _ spec.Versioned = Codec{}
}

// validNodeV10 is a v1.0.3-shaped Node: top-level href required, NO
// description, NO tags, NO api, NO clocks, NO interfaces.
func validNodeV10(id string) is04.Node {
	return is04.Node{
		ResourceCore: is04.ResourceCore{
			ID:      id,
			Version: "1700000000:0",
			Label:   "v1.0 Node",
		},
		Hostname: "dhs.local",
		Href:     "http://dhs.local:8080/",
		Caps:     map[string]any{},
		Services: []is04.NodeService{},
	}
}

func TestNodeEncodeStripsV11PlusFields(t *testing.T) {
	c := Codec{}
	n := validNodeV10("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	// Caller might populate v1.1+ fields; v1.0 encode must drop them all.
	n.Description = "should not appear on v1.0 wire"
	n.Tags = map[string][]string{"k": {"v"}}
	n.API = is04.NodeAPI{Versions: []string{"v1.0"}, Endpoints: []is04.NodeEndpoint{{Host: "h", Port: 8080, Protocol: "http"}}}
	n.Clocks = []is04.NodeClock{{Name: "clk0", RefType: "internal"}}
	chassis := "00-11-22-33-44-55"
	n.Interfaces = []is04.NodeIface{{Name: "eth0", ChassisID: &chassis, PortID: "00-11-22-33-44-66"}}

	body, err := c.EncodeNode(n)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	for _, k := range nodeV11PlusFields {
		if _, present := m[k]; present {
			t.Fatalf("v1.0 wire must not carry %q: %s", k, body)
		}
	}
	// The v1.0 required fields must still be present.
	for _, k := range []string{"id", "version", "label", "href", "caps", "services"} {
		if _, present := m[k]; !present {
			t.Fatalf("v1.0 wire must carry %q: %s", k, body)
		}
	}
}

func TestNodeDecodeRejectsV11PlusFields(t *testing.T) {
	c := Codec{}
	// description and tags are NOT v1.1+ — v1.0.3 schema treats them
	// as permitted additionalProperties, and AMWA test_28 expects
	// `tags` to round-trip from the bundle on /x-nmos/node/v1.0/.
	// Only api / clocks / interfaces are genuinely v1.1+ keys.
	cases := []string{
		`"api":{"versions":["v1.0"],"endpoints":[]}`,
		`"clocks":[]`,
		`"interfaces":[]`,
	}
	for _, extra := range cases {
		body := []byte(`{
		  "id":"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		  "version":"1700000000:0","label":"x","href":"http://h/",
		  "caps":{},"services":[],
		  ` + extra + `}`)
		if _, err := c.DecodeNode(body); err == nil {
			t.Fatalf("expected rejection of %q", extra)
		}
	}
}

func TestNodeValidateAcceptsMinimalV10(t *testing.T) {
	c := Codec{}
	n := validNodeV10("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	if err := c.ValidateNode(n); err != nil {
		t.Fatalf("v1.0 validator should accept minimal Node (no description/tags/api/clocks/interfaces): %v", err)
	}
}

func TestNodeValidateRejectsBadUUID(t *testing.T) {
	c := Codec{}
	n := validNodeV10("not-a-uuid")
	if err := c.ValidateNode(n); err == nil {
		t.Fatalf("expected v1.0 validator to reject non-UUID id")
	}
}

// validDeviceV10 is a v1.0.3-shaped Device — NO description, NO tags,
// NO controls.
func validDeviceV10(id string) is04.Device {
	return is04.Device{
		ResourceCore: is04.ResourceCore{
			ID:      id,
			Version: "1700000000:0",
			Label:   "v1.0 Device",
		},
		Type:      "urn:x-nmos:device:generic",
		NodeID:    "abcdef01-1234-4abc-9def-1234567890ab",
		Senders:   []string{},
		Receivers: []string{},
	}
}

func TestDeviceEncodeStripsV11PlusFields(t *testing.T) {
	c := Codec{}
	d := validDeviceV10("12345678-1234-4abc-9def-1234567890ab")
	d.Description = "should drop"
	d.Tags = map[string][]string{}
	body, err := c.EncodeDevice(d)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	for _, k := range deviceV11PlusFields {
		if _, present := m[k]; present {
			t.Fatalf("v1.0 wire must not carry %q: %s", k, body)
		}
	}
}

func TestDeviceDecodeRejectsControls(t *testing.T) {
	c := Codec{}
	body := []byte(`{
	  "id":"12345678-1234-4abc-9def-1234567890ab",
	  "version":"1700000000:0","label":"x",
	  "type":"urn:x-nmos:device:generic",
	  "node_id":"abcdef01-1234-4abc-9def-1234567890ab",
	  "senders":[],"receivers":[],
	  "controls":[{"href":"http://h","type":"urn:x-nmos:control:sr-ctrl/v1.0"}]
	}`)
	if _, err := c.DecodeDevice(body); err == nil {
		t.Fatalf("expected v1.0 decoder to reject Device with `controls`")
	} else if !strings.Contains(err.Error(), "controls") {
		t.Fatalf("error should name the rejected field, got %v", err)
	}
}

// validSourceV10 — Source v1.0 has description + tags + parents.
func validSourceV10(id string) is04.Source {
	return is04.Source{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "v1.0 Source",
			Description: "fixture",
			Tags:        map[string][]string{},
		},
		Format:   "urn:x-nmos:format:video",
		Caps:     map[string]any{},
		DeviceID: "abcdef01-1234-4abc-9def-1234567890ab",
		Parents:  []string{},
	}
}

func TestSourceEncodeStripsV11PlusFields(t *testing.T) {
	c := Codec{}
	s := validSourceV10("11111111-1111-4111-8111-111111111111")
	body, err := c.EncodeSource(s)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	for _, k := range sourceV11PlusFields {
		if _, present := m[k]; present {
			t.Fatalf("v1.0 wire must not carry %q: %s", k, body)
		}
	}
}

func TestSourceDecodeRejectsClockName(t *testing.T) {
	c := Codec{}
	body := []byte(`{
	  "id":"11111111-1111-4111-8111-111111111111",
	  "version":"1700000000:0","label":"x","description":"x","tags":{},
	  "format":"urn:x-nmos:format:video","caps":{},
	  "device_id":"abcdef01-1234-4abc-9def-1234567890ab",
	  "parents":[],
	  "clock_name":"clk0"
	}`)
	if _, err := c.DecodeSource(body); err == nil {
		t.Fatalf("expected v1.0 decoder to reject Source with `clock_name`")
	}
}

// validFlowV10 — Flow v1.0 has description + tags + parents, NO
// device_id, NO grain_rate, NO media_type, NO per-format fields.
func validFlowV10(id string) is04.Flow {
	return is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "v1.0 Flow",
			Description: "fixture",
			Tags:        map[string][]string{},
		},
		Format:   "urn:x-nmos:format:video",
		SourceID: "11111111-1111-4111-8111-111111111111",
		Parents:  []string{},
	}
}

func TestFlowEncodeStripsV11PlusFields(t *testing.T) {
	c := Codec{}
	f := validFlowV10("22222222-2222-4222-8222-222222222222")
	// Caller fills v1.1+ fields; v1.0 encode must drop all.
	f.DeviceID = "abcdef01-1234-4abc-9def-1234567890ab"
	f.MediaType = "video/raw"
	f.FrameWidth = 1920
	f.FrameHeight = 1080
	body, err := c.EncodeFlow(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	for _, k := range flowV11PlusFields {
		if _, present := m[k]; present {
			t.Fatalf("v1.0 wire must not carry %q: %s", k, body)
		}
	}
}

func TestFlowDecodeRejectsMediaType(t *testing.T) {
	c := Codec{}
	body := []byte(`{
	  "id":"22222222-2222-4222-8222-222222222222",
	  "version":"1700000000:0","label":"x","description":"x","tags":{},
	  "format":"urn:x-nmos:format:video",
	  "source_id":"11111111-1111-4111-8111-111111111111",
	  "parents":[],
	  "media_type":"video/raw"
	}`)
	if _, err := c.DecodeFlow(body); err == nil {
		t.Fatalf("expected v1.0 decoder to reject Flow with `media_type`")
	}
}

// validSenderV10 — Sender v1.0: flow_id + manifest_href both REQUIRED
// non-null. NO caps, NO interface_bindings, NO subscription.
func validSenderV10(id string) is04.Sender {
	flowID := "33333333-3333-4333-8333-333333333333"
	manifest := "http://dhs.local:8080/x-nmos/node/v1.0/senders/" + id + "/manifest"
	return is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "v1.0 Sender",
			Description: "fixture",
			Tags:        map[string][]string{},
		},
		FlowID:       &flowID,
		Transport:    "urn:x-nmos:transport:rtp",
		DeviceID:     "abcdef01-1234-4abc-9def-1234567890ab",
		ManifestHref: &manifest,
	}
}

func TestSenderEncodeStripsV12PlusFields(t *testing.T) {
	c := Codec{}
	s := validSenderV10("44444444-4444-4444-8444-444444444444")
	rid := "55555555-5555-4555-8555-555555555555"
	s.Caps = map[string]any{"x": 1}
	s.InterfaceBindings = []string{"eth0"}
	s.Subscription = is04.SenderSubscription{ReceiverID: &rid, Active: true}
	body, err := c.EncodeSender(s)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	for _, k := range senderV11PlusFields {
		if _, present := m[k]; present {
			t.Fatalf("v1.0 wire must not carry %q: %s", k, body)
		}
	}
}

func TestSenderDecodeRejectsCaps(t *testing.T) {
	c := Codec{}
	body := []byte(`{
	  "id":"44444444-4444-4444-8444-444444444444",
	  "version":"1700000000:0","label":"x","description":"x","tags":{},
	  "flow_id":"33333333-3333-4333-8333-333333333333",
	  "transport":"urn:x-nmos:transport:rtp",
	  "device_id":"abcdef01-1234-4abc-9def-1234567890ab",
	  "manifest_href":"http://h/m",
	  "caps":{}
	}`)
	if _, err := c.DecodeSender(body); err == nil {
		t.Fatalf("expected v1.0 decoder to reject Sender with `caps`")
	}
}

func TestSenderValidateRejectsNullFlowID(t *testing.T) {
	c := Codec{}
	s := validSenderV10("44444444-4444-4444-8444-444444444444")
	s.FlowID = nil
	if err := c.ValidateSender(s); err == nil {
		t.Fatalf("v1.0 validator must require non-null flow_id")
	}
}

func TestSenderValidateRejectsNullManifestHref(t *testing.T) {
	c := Codec{}
	s := validSenderV10("44444444-4444-4444-8444-444444444444")
	s.ManifestHref = nil
	if err := c.ValidateSender(s); err == nil {
		t.Fatalf("v1.0 validator must require non-null manifest_href")
	}
}

// validReceiverV10 — Receiver v1.0 has subscription required, but
// sender_id may be null.
func validReceiverV10(id string) is04.Receiver {
	return is04.Receiver{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "v1.0 Receiver",
			Description: "fixture",
			Tags:        map[string][]string{},
		},
		Format:    "urn:x-nmos:format:video",
		Caps:      is04.ReceiverCaps{},
		DeviceID:  "abcdef01-1234-4abc-9def-1234567890ab",
		Transport: "urn:x-nmos:transport:rtp",
		Subscription: is04.ReceiverSubscription{
			SenderID: nil,
			Active:   false,
		},
	}
}

func TestReceiverEncodeStripsInterfaceBindings(t *testing.T) {
	c := Codec{}
	r := validReceiverV10("66666666-6666-4666-8666-666666666666")
	r.InterfaceBindings = []string{"eth0"}
	body, err := c.EncodeReceiver(r)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if _, present := m["interface_bindings"]; present {
		t.Fatalf("v1.0 wire must not carry `interface_bindings`: %s", body)
	}
}

func TestReceiverDecodeRejectsInterfaceBindings(t *testing.T) {
	c := Codec{}
	body := []byte(`{
	  "id":"66666666-6666-4666-8666-666666666666",
	  "version":"1700000000:0","label":"x","description":"x","tags":{},
	  "format":"urn:x-nmos:format:video","caps":{},
	  "device_id":"abcdef01-1234-4abc-9def-1234567890ab",
	  "transport":"urn:x-nmos:transport:rtp",
	  "subscription":{"sender_id":null,"active":false},
	  "interface_bindings":["eth0"]
	}`)
	if _, err := c.DecodeReceiver(body); err == nil {
		t.Fatalf("expected v1.0 decoder to reject Receiver with `interface_bindings`")
	}
}

func TestRejectFieldsHelperOnNonObjectJSON(t *testing.T) {
	if err := rejectFields([]byte("[1,2,3]"), []string{"x"}, "node"); err == nil {
		t.Fatalf("rejectFields should reject non-object JSON")
	}
}

// TestRoundTripNodeV10 verifies encode → decode round-trips a v1.0 Node
// without losing information.
func TestRoundTripNodeV10(t *testing.T) {
	c := Codec{}
	n := validNodeV10("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	body, err := c.EncodeNode(n)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.DecodeNode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != n.ID || got.Label != n.Label || got.Href != n.Href {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, n)
	}
}
