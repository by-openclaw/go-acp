package is04

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

func validNode() Node {
	chassis := "ff-ff-ff-ff-ff-ff"
	return Node{
		ResourceCore: ResourceCore{
			ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
			Version:     "1441700172:318426300",
			Label:       "lab-node",
			Description: "lab fixture",
			Tags:        map[string][]string{},
		},
		Href: "http://10.6.239.113:8080/",
		Caps: map[string]any{},
		API: NodeAPI{
			Versions: []string{"v1.3"},
			Endpoints: []NodeEndpoint{
				{Host: "10.6.239.113", Port: 8080, Protocol: "http"},
			},
		},
		Services: []NodeService{},
		Clocks: []NodeClock{
			{Name: "clk0", RefType: "internal"},
		},
		Interfaces: []NodeIface{
			{ChassisID: &chassis, PortID: "ff-ff-ff-ff-ff-ff", Name: "eth0"},
		},
	}
}

func TestNodeValidateHappy(t *testing.T) {
	n := validNode()
	if err := n.Validate(); err != nil {
		t.Fatalf("valid Node rejected: %v", err)
	}
}

func TestNodeRoundTrip(t *testing.T) {
	in := validNode()
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeNode(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != in.ID || got.API.Versions[0] != "v1.3" {
		t.Fatalf("round-trip diverged: %+v", got)
	}
}

func TestNodeRequiredMissing(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Node)
		want   string
	}{
		{"href missing", func(n *Node) { n.Href = "" }, "href"},
		{"caps missing", func(n *Node) { n.Caps = nil }, "caps"},
		// `versions empty`, `endpoints empty`, `clocks missing`,
		// `interfaces missing` are intentionally NOT error cases in
		// the canonical validator: v1.0.3 Node schema has no `api`
		// property at all (added in v1.1), no `clocks` (added in
		// v1.1), no `interfaces` (added in v1.2), so a v1.0 wire
		// shape decoded into the canonical struct has zero-value
		// API/Clocks/Interfaces. Strict per-version presence
		// requirements live in
		// `internal/amwa/registry/store.go validateRegistrationPresenceVersioned`.
		{"bad protocol", func(n *Node) { n.API.Endpoints[0].Protocol = "ftp" }, "protocol"},
		{"port out of range", func(n *Node) { n.API.Endpoints[0].Port = 70000 }, "port"},
		{"interface bad mac", func(n *Node) { n.Interfaces[0].PortID = "not-a-mac" }, "port_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := validNode()
			tc.mutate(&n)
			err := n.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// TestNodeUnknownFieldAbsorbedAndReported: a field IS-04 does not
// define is a DEVIATION, not a failure.
//
// This test previously asserted the opposite — that decode rejects the
// payload. That contract is wrong for a consumer and it broke against
// real hardware: an EVS Neuron sends `caps` on Flows and `ip_addr` on
// the Node, and strict decoding refused 144 of 176 Flows and the
// device's own `self`. Refusing to read a device because it carries a
// vendor extension is not strictness, it is blindness. The deviation
// is now recorded so it stays auditable.
func TestNodeUnknownFieldAbsorbedAndReported(t *testing.T) {
	in := validNode()
	wire, _ := in.Encode()
	bad := strings.Replace(string(wire), `"href":`, `"rogue_field": "x", "href":`, 1)

	var rep spec.SliceReporter
	got, err := DecodeNodeReporting([]byte(bad), APIVersion, &rep)
	if err != nil {
		t.Fatalf("an unknown field must not fail the decode: %v", err)
	}
	if got.ID != in.ID {
		t.Errorf("id = %q, want %q — the rest of the resource must survive", got.ID, in.ID)
	}

	events := rep.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d compliance events, want exactly 1", len(events))
	}
	if events[0].Code != UnknownFieldCode {
		t.Errorf("code = %q, want %q", events[0].Code, UnknownFieldCode)
	}
	if !strings.Contains(events[0].Detail, "rogue_field") {
		t.Errorf("the event must name the field; got %q", events[0].Detail)
	}
}

// TestUnknownFieldSilentWithoutReporter: the plain Decode* entry
// points keep working for callers that have no reporter to give.
func TestUnknownFieldSilentWithoutReporter(t *testing.T) {
	in := validNode()
	wire, _ := in.Encode()
	bad := strings.Replace(string(wire), `"href":`, `"rogue_field": "x", "href":`, 1)
	if _, err := DecodeNode([]byte(bad)); err != nil {
		t.Fatalf("DecodeNode must absorb an unknown field: %v", err)
	}
}

// TestMalformedInputStillFails: absorbing unknown FIELDS must not
// weaken anything else. Broken JSON, a wrong type, and trailing
// content are all still errors.
func TestMalformedInputStillFails(t *testing.T) {
	cases := map[string]string{
		"broken json":      `{"id":`,
		"wrong type":       `{"id": 42}`,
		"trailing content": `{"id":"f47ac10b-58cc-4372-a567-0e02b2c3d479"} {"more":1}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeNode([]byte(body)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func validDevice() Device {
	return Device{
		ResourceCore: ResourceCore{
			ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
			Version:     "0:0",
			Label:       "lab-dev",
			Description: "lab fixture",
			Tags:        map[string][]string{},
		},
		Type:      "urn:x-nmos:device:generic",
		NodeID:    "3b8be755-08ff-452b-b217-c9151eb21193",
		Senders:   []string{},
		Receivers: []string{},
		Controls:  []DeviceControl{},
	}
}

func TestDeviceValidateHappy(t *testing.T) {
	d := validDevice()
	if err := d.Validate(); err != nil {
		t.Fatalf("valid Device rejected: %v", err)
	}
}

func TestDeviceTypeURN(t *testing.T) {
	d := validDevice()
	d.Type = "urn:x-nmos:UNKNOWN-NAMESPACE:foo"
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("urn:x-nmos:* with bad sub-namespace must reject: %v", err)
	}
	d.Type = "https://vendor.example/device/typeA"
	if err := d.Validate(); err != nil {
		t.Fatalf("non-NMOS URI should pass: %v", err)
	}
	d.Type = "urn:x-nmos:device:generic"
	if err := d.Validate(); err != nil {
		t.Fatalf("urn:x-nmos:device:* should pass: %v", err)
	}
}

func validSource() Source {
	clk := "clk0"
	return Source{
		ResourceCore: ResourceCore{
			ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
			Version:     "0:0",
			Label:       "src-1",
			Description: "lab src",
			Tags:        map[string][]string{},
		},
		Caps:      map[string]any{},
		DeviceID:  "3b8be755-08ff-452b-b217-c9151eb21193",
		Parents:   []string{},
		ClockName: &clk,
		Format:    FormatVideo,
	}
}

func TestSourceClockNameNullable(t *testing.T) {
	// Wire form with explicit null — this is spec-compliant.
	raw := `{
  "id":"3b8be755-08ff-452b-b217-c9151eb21193",
  "version":"0:0",
  "label":"x", "description":"y", "tags":{},
  "caps":{}, "device_id":"3b8be755-08ff-452b-b217-c9151eb21193",
  "parents":[], "clock_name":null, "format":"urn:x-nmos:format:video"
}`
	if _, err := DecodeSource([]byte(raw)); err != nil {
		t.Fatalf("clock_name=null should decode: %v", err)
	}
	// Wire form with a real clock name — also spec-compliant.
	raw2 := strings.Replace(raw, `"clock_name":null`, `"clock_name":"clk0"`, 1)
	if _, err := DecodeSource([]byte(raw2)); err != nil {
		t.Fatalf("clock_name=\"clk0\" should decode: %v", err)
	}
}

func TestSourceAudioChannelsValidatedWhenPresent(t *testing.T) {
	// canonical Validate is lenient when `channels` is absent on an
	// audio Source (v1.0 audio Source has no channels field at all;
	// added in v1.1). Strict per-version presence lives in registry's
	// `validateRegistrationPresenceVersioned`. Here we only confirm
	// the per-element validation fires when channels ARE present.
	s := validSource()
	s.Format = FormatAudio
	s.Channels = []SourceAudioChannel{{Label: ""}} // bad label
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "channels[0].label") {
		t.Fatalf("audio source with empty channel label should reject: %v", err)
	}
	s.Channels = []SourceAudioChannel{{Label: "L"}}
	if err := s.Validate(); err != nil {
		t.Fatalf("audio source with valid channels rejected: %v", err)
	}
}

func validFlow() Flow {
	return Flow{
		ResourceCore: ResourceCore{
			ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
			Version:     "0:0",
			Label:       "flow-1",
			Description: "lab flow",
			Tags:        map[string][]string{},
		},
		SourceID:    "3b8be755-08ff-452b-b217-c9151eb21193",
		DeviceID:    "3b8be755-08ff-452b-b217-c9151eb21193",
		Parents:     []string{},
		Format:      FormatVideo,
		FrameWidth:  1920,
		FrameHeight: 1080,
		Interlace:   "progressive",
	}
}

func TestFlowValidateHappy(t *testing.T) {
	f := validFlow()
	if err := f.Validate(); err != nil {
		t.Fatalf("valid Flow rejected: %v", err)
	}
}

func TestFlowVideoRequiresFrameSize(t *testing.T) {
	f := validFlow()
	f.FrameWidth = 0
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "frame_width") {
		t.Fatalf("video flow without frame_width should reject: %v", err)
	}
}

func TestFlowAudioRequiresSampleRate(t *testing.T) {
	f := validFlow()
	f.Format = FormatAudio
	f.FrameWidth = 0
	f.FrameHeight = 0
	f.Interlace = ""
	// Set bit_depth to trigger the canonical "audio-fields-present"
	// branch that demands sample_rate. canonical Validate is lenient
	// when ALL audio fields are absent (v1.0 audio Flow has no
	// per-format breakdown); strict per-version presence lives in
	// registry's `validateRegistrationPresenceVersioned`.
	f.BitDepth = 24
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "sample_rate") {
		t.Fatalf("audio flow with bit_depth but no sample_rate should reject: %v", err)
	}
	f.SampleRate = &GrainRate{Numerator: 48000, Denominator: 1}
	if err := f.Validate(); err != nil {
		t.Fatalf("audio flow with sample_rate rejected: %v", err)
	}
}

func validSender() Sender {
	href := "http://10.6.239.113:8080/x-nmos/node/v1.3/senders/abc/manifest"
	flow := "3b8be755-08ff-452b-b217-c9151eb21193"
	return Sender{
		ResourceCore: ResourceCore{
			ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
			Version:     "0:0",
			Label:       "snd-1",
			Description: "lab snd",
			Tags:        map[string][]string{},
		},
		FlowID:            &flow,
		Transport:         TransportRTPMcast,
		DeviceID:          "3b8be755-08ff-452b-b217-c9151eb21193",
		ManifestHref:      &href,
		InterfaceBindings: []string{"eth0"},
		Subscription:      SenderSubscription{Active: false},
	}
}

func TestSenderValidateHappy(t *testing.T) {
	s := validSender()
	if err := s.Validate(); err != nil {
		t.Fatalf("valid Sender rejected: %v", err)
	}
}

func TestSenderTransportURN(t *testing.T) {
	s := validSender()
	s.Transport = "urn:x-nmos:transport:UNKNOWN"
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("unknown urn:x-nmos:transport:* must reject: %v", err)
	}
	s.Transport = "https://vendor.example/transport/typeA"
	if err := s.Validate(); err != nil {
		t.Fatalf("non-NMOS transport URI should pass: %v", err)
	}
}

func TestSenderManifestNullOK(t *testing.T) {
	s := validSender()
	s.ManifestHref = nil
	if err := s.Validate(); err != nil {
		t.Fatalf("manifest_href=nil should pass: %v", err)
	}
}

func validReceiver() Receiver {
	return Receiver{
		ResourceCore: ResourceCore{
			ID:          "3b8be755-08ff-452b-b217-c9151eb21193",
			Version:     "0:0",
			Label:       "rcv-1",
			Description: "lab rcv",
			Tags:        map[string][]string{},
		},
		DeviceID:          "3b8be755-08ff-452b-b217-c9151eb21193",
		Transport:         TransportRTPMcast,
		InterfaceBindings: []string{"eth0"},
		Format:            FormatVideo,
		Caps:              ReceiverCaps{MediaTypes: []string{"video/raw"}},
		Subscription:      ReceiverSubscription{Active: false},
	}
}

func TestReceiverValidateHappy(t *testing.T) {
	r := validReceiver()
	if err := r.Validate(); err != nil {
		t.Fatalf("valid Receiver rejected: %v", err)
	}
}

func TestReceiverFormatRequired(t *testing.T) {
	r := validReceiver()
	r.Format = ""
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "format") {
		t.Fatalf("receiver missing format must reject: %v", err)
	}
}

func TestRegistrationEnvelope(t *testing.T) {
	body, err := EncodeRegistration(ResourceNode, validNode())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	r, err := DecodeRegistration(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Type != ResourceNode {
		t.Fatalf("type = %q", r.Type)
	}
	// Inner data round-trips into a Node.
	var got Node
	if err := json.Unmarshal(r.Data, &got); err != nil {
		t.Fatalf("inner unmarshal: %v", err)
	}
	if got.ID != "3b8be755-08ff-452b-b217-c9151eb21193" {
		t.Fatalf("inner id = %q", got.ID)
	}
}

func TestRegistrationRejectsBadType(t *testing.T) {
	if _, err := EncodeRegistration(ResourceType("bogus"), nil); err == nil {
		t.Fatal("expected invalid-type rejection")
	}
	bad := []byte(`{"type":"bogus","data":{}}`)
	if _, err := DecodeRegistration(bad); err == nil {
		t.Fatal("expected decode rejection on bogus type")
	}
}

func TestPluralCollections(t *testing.T) {
	want := map[ResourceType]string{
		ResourceNode:     "nodes",
		ResourceDevice:   "devices",
		ResourceSource:   "sources",
		ResourceFlow:     "flows",
		ResourceSender:   "senders",
		ResourceReceiver: "receivers",
	}
	for r, w := range want {
		if got := r.Plural(); got != w {
			t.Fatalf("%s.Plural() = %q, want %q", r, got, w)
		}
	}
}
