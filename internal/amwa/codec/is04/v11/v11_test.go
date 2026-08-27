package v11

import (
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/spec"
	"encoding/json"
	"testing"
)

func TestCodecVersionedShape(t *testing.T) {
	c := Codec{}
	if c.SpecID() != "is-04" {
		t.Fatalf("SpecID = %q", c.SpecID())
	}
	if c.APIVer() != "v1.1" {
		t.Fatalf("APIVer = %q", c.APIVer())
	}
	if c.SpecPatch() != "v1.1.3" {
		t.Fatalf("SpecPatch = %q", c.SpecPatch())
	}
}

func TestCodecRegistersOnInit(t *testing.T) {
	got, ok := is04.Get("v1.1")
	if !ok {
		t.Fatalf("is04.Get(v1.1) miss — init() did not register")
	}
	if got.APIVer() != "v1.1" {
		t.Fatalf("registered APIVer = %q", got.APIVer())
	}
}

func TestCodecImplementsInterface(t *testing.T) {
	var _ is04.Codec = Codec{}
	var _ spec.Versioned = Codec{}
}

// validNodeV11 is a v1.1.3-shaped Node: NO Interfaces field (added
// in v1.2). On the wire, the JSON must not carry the `interfaces`
// key — Encode strips it.
func validNodeV11(id string) is04.Node {
	return is04.Node{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "v1.1 Node",
			Description: "v1.1 fixture",
			Tags:        map[string][]string{},
		},
		Hostname: "dhs.local",
		Href:     "http://dhs.local:8080/",
		Caps:     map[string]any{},
		API: is04.NodeAPI{
			Versions:  []string{"v1.1"},
			Endpoints: []is04.NodeEndpoint{{Host: "dhs.local", Port: 8080, Protocol: "http"}},
		},
		Services: []is04.NodeService{},
		Clocks:   []is04.NodeClock{},
		// Interfaces left nil — v1.1 wire MUST NOT carry it.
	}
}

func TestNodeEncodeStripsV12Fields(t *testing.T) {
	c := Codec{}
	chassis := "00-11-22-33-44-55"
	n := validNodeV11("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	// Even if a caller fills Interfaces, v1.1 encode must drop them.
	n.Interfaces = []is04.NodeIface{{Name: "eth0", ChassisID: &chassis, PortID: "00-11-22-33-44-66"}}
	body, err := c.EncodeNode(n)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if _, present := m["interfaces"]; present {
		t.Fatalf("v1.1 wire must not carry `interfaces`: %s", body)
	}
}

func TestNodeDecodeAbsorbsV12Fields(t *testing.T) {
	body := []byte(`{
	  "id":"f47ac10b-58cc-4372-a567-0e02b2c3d479",
	  "version":"1700000000:0","label":"x","description":"x",
	  "tags":{},
	  "hostname":"h","href":"http://h/","caps":{},
	  "api":{"versions":["v1.1"],"endpoints":[]},
	  "services":[],"clocks":[],
	  "interfaces":[{"name":"eth0","chassis_id":"00-11-22-33-44-55","port_id":"00-11-22-33-44-66"}]
	}`)
	rep := &spec.SliceReporter{}
	if _, err := (Codec{Reporter: rep}).DecodeNode(body); err != nil {
		t.Fatalf("a later-minor field must be absorbed, not rejected: %v", err)
	}
	// AMWA's v1.1 schema does NOT forbid this property — there is no
	// additionalProperties:false on these resources. So the correct
	// behaviour is to accept it in silence. Our old hand-written rule
	// rejected it, and that rule was invented, not specified.
	if n := len(rep.Snapshot()); n != 0 {
		t.Fatalf("AMWA permits this on a v1.1 tree; %d deviations reported: %v",
			n, rep.Snapshot())
	}
}

func TestNodeValidateAcceptsMissingInterfaces(t *testing.T) {
	c := Codec{}
	n := validNodeV11("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	if err := c.ValidateNode(n); err != nil {
		t.Fatalf("v1.1 validator should accept Node without Interfaces: %v", err)
	}
}

// validSenderV11 is a v1.1.3-shaped Sender — no caps /
// interface_bindings / subscription. flow_id and manifest_href are
// non-null pointers carrying valid values.
func validSenderV11(id string) is04.Sender {
	flowID := "12345678-1234-4abc-9def-1234567890ab"
	manifest := "http://dhs.local:8080/x-nmos/node/v1.1/senders/" + id + "/transportfile"
	return is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID:          id,
			Version:     "1700000000:0",
			Label:       "v1.1 Sender",
			Description: "fixture",
			Tags:        map[string][]string{},
		},
		FlowID:       &flowID,
		Transport:    "urn:x-nmos:transport:rtp",
		DeviceID:     "abcdef01-1234-4abc-9def-1234567890ab",
		ManifestHref: &manifest,
	}
}

func TestSenderEncodeStripsV12Fields(t *testing.T) {
	c := Codec{}
	s := validSenderV11("11111111-1111-4111-8111-111111111111")
	// Caller might fill v1.2+ fields; v1.1 encode must drop all of them.
	rid := "22222222-2222-4222-8222-222222222222"
	s.Caps = map[string]any{"k": "v"}
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
	for _, k := range []string{"caps", "interface_bindings", "subscription"} {
		if _, present := m[k]; present {
			t.Fatalf("v1.1 wire must not carry %q: %s", k, body)
		}
	}
}

func TestSenderDecodeAbsorbsV12Fields(t *testing.T) {
	cases := []string{`"caps":{}`, `"interface_bindings":[]`, `"subscription":{"active":false}`}
	for _, extra := range cases {
		body := []byte(`{
		  "id":"11111111-1111-4111-8111-111111111111",
		  "version":"1700000000:0","label":"x","description":"x","tags":{},
		  "flow_id":"12345678-1234-4abc-9def-1234567890ab",
		  "transport":"urn:x-nmos:transport:rtp",
		  "device_id":"abcdef01-1234-4abc-9def-1234567890ab",
		  "manifest_href":"http://h/m",
		  ` + extra + `}`)
		rep := &spec.SliceReporter{}
		if _, err := (Codec{Reporter: rep}).DecodeSender(body); err != nil {
			t.Fatalf("a later-minor field must be absorbed, not rejected: %v", err)
		}
		// AMWA's v1.1 schema does NOT forbid this property — there is no
		// additionalProperties:false on these resources. So the correct
		// behaviour is to accept it in silence. Our old hand-written rule
		// rejected it, and that rule was invented, not specified.
		if n := len(rep.Snapshot()); n != 0 {
			t.Fatalf("AMWA permits this on a v1.1 tree; %d deviations reported: %v",
				n, rep.Snapshot())
		}
	}
}

func TestSenderValidateAcceptsMinimalV11(t *testing.T) {
	c := Codec{}
	s := validSenderV11("11111111-1111-4111-8111-111111111111")
	if err := c.ValidateSender(s); err != nil {
		t.Fatalf("v1.1 sender validator should accept minimal fixture: %v", err)
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
		if e.APIVer != "v1.1" {
			t.Errorf("apiVer = %q", e.APIVer)
		}
	}
}
