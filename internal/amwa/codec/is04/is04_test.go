package is04

import (
	"encoding/json"
	"strings"
	"testing"
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
		{"versions empty", func(n *Node) { n.API.Versions = nil }, "api.versions"},
		{"endpoints empty", func(n *Node) { n.API.Endpoints = nil }, "api.endpoints"},
		{"bad protocol", func(n *Node) { n.API.Endpoints[0].Protocol = "ftp" }, "protocol"},
		{"port out of range", func(n *Node) { n.API.Endpoints[0].Port = 70000 }, "port"},
		// `clocks missing` and `interfaces missing` are intentionally
		// not error cases in the canonical validator: clocks landed in
		// IS-04 v1.1 and interfaces in v1.2, so a v1.0 fixture has
		// neither. Per-version presence checks live in
		// `internal/amwa/registry/store.go validateRegistrationPresenceVersioned`.
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

func TestNodeUnknownFieldRejected(t *testing.T) {
	in := validNode()
	wire, _ := in.Encode()
	bad := strings.Replace(string(wire), `"href":`, `"rogue_field": "x", "href":`, 1)
	if _, err := DecodeNode([]byte(bad)); err == nil || !strings.Contains(err.Error(), "rogue_field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
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

func TestSourceAudioRequiresChannels(t *testing.T) {
	s := validSource()
	s.Format = FormatAudio
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "channels") {
		t.Fatalf("audio source without channels must reject: %v", err)
	}
	s.Channels = []SourceAudioChannel{{Label: "L"}}
	if err := s.Validate(); err != nil {
		t.Fatalf("audio source with channels rejected: %v", err)
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
		SourceID:  "3b8be755-08ff-452b-b217-c9151eb21193",
		DeviceID:  "3b8be755-08ff-452b-b217-c9151eb21193",
		Parents:   []string{},
		Format:    FormatVideo,
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
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "sample_rate") {
		t.Fatalf("audio flow without sample_rate should reject: %v", err)
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
