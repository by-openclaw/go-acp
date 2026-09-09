package is04

import (
	"encoding/json"
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

// wantValidation asserts err names the offending field the way an
// operator would grep for it in a log line.
func wantValidation(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a validation error naming %q, got nil", field)
	}
	if !strings.Contains(err.Error(), field) {
		t.Fatalf("error must name %q, got: %v", field, err)
	}
}

// TestNodeValidateNamesEveryBrokenField: each case breaks exactly one
// rule of node.json and the error must point at that field by path.
func TestNodeValidateNamesEveryBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Node)
		field  string
	}{
		{"id is not a UUID", func(n *Node) { n.ID = "nope" }, "node.id"},
		{"version is not TAI", func(n *Node) { n.Version = "now" }, "node.version"},
		{"api version is not vMAJOR.MINOR", func(n *Node) { n.API.Versions[0] = "1.3" }, "node.api.versions[0]"},
		{"endpoint host missing", func(n *Node) { n.API.Endpoints[0].Host = "" }, "node.api.endpoints[0].host"},
		{"service href missing", func(n *Node) {
			n.Services = []NodeService{{Type: "urn:x-vendor:service:x"}}
		}, "node.services[0].href"},
		{"service type missing", func(n *Node) {
			n.Services = []NodeService{{Href: "http://dhs.local/svc"}}
		}, "node.services[0].type"},
		{"clock name missing", func(n *Node) { n.Clocks[0].Name = "" }, "node.clocks[0].name"},
		{"clock ref_type outside internal|ptp", func(n *Node) { n.Clocks[0].RefType = "gps" }, "node.clocks[0].ref_type"},
		{"interface name missing", func(n *Node) { n.Interfaces[0].Name = "" }, "node.interfaces[0].name"},
		{"interface chassis_id empty string", func(n *Node) { n.Interfaces[0].ChassisID = strp("") }, "node.interfaces[0].chassis_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := validNode()
			tc.mutate(&n)
			wantValidation(t, n.Validate(), tc.field)
		})
	}
}

// TestNodeEncodeRefusesInvalid: Encode validates first, so an invalid
// Node never reaches the wire.
func TestNodeEncodeRefusesInvalid(t *testing.T) {
	n := validNode()
	n.Href = ""
	_, err := n.Encode()
	wantValidation(t, err, "node.href")
}

func TestDeviceValidateNamesEveryBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Device)
		field  string
	}{
		{"node_id is not a UUID", func(d *Device) { d.NodeID = "node-1" }, "device.node_id"},
		{"senders entry is not a UUID", func(d *Device) { d.Senders = []string{"snd"} }, "device.senders[0]"},
		{"receivers entry is not a UUID", func(d *Device) { d.Receivers = []string{"rcv"} }, "device.receivers[0]"},
		{"control href missing", func(d *Device) {
			d.Controls = []DeviceControl{{Type: "urn:x-nmos:control:sr-ctrl/v1.1"}}
		}, "device.controls[0].href"},
		{"control type missing", func(d *Device) {
			d.Controls = []DeviceControl{{Href: "http://dhs.local/x-nmos/connection/"}}
		}, "device.controls[0].type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDevice()
			tc.mutate(&d)
			wantValidation(t, d.Validate(), tc.field)
		})
	}
}

func TestSourceValidateNamesEveryBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Source)
		field  string
	}{
		{"caps missing", func(s *Source) { s.Caps = nil }, "source.caps"},
		{"device_id is not a UUID", func(s *Source) { s.DeviceID = "dev" }, "source.device_id"},
		{"parents missing", func(s *Source) { s.Parents = nil }, "source.parents"},
		{"parents entry is not a UUID", func(s *Source) { s.Parents = []string{"p"} }, "source.parents[0]"},
		{"grain_rate numerator not positive", func(s *Source) { s.GrainRate = &GrainRate{Numerator: 0} }, "source.grain_rate.numerator"},
		{"grain_rate denominator negative", func(s *Source) {
			s.GrainRate = &GrainRate{Numerator: 25, Denominator: -1}
		}, "source.grain_rate.denominator"},
		{"format is an unknown x-nmos URN", func(s *Source) { s.Format = "urn:x-nmos:format:hologram" }, "source.format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSource()
			tc.mutate(&s)
			wantValidation(t, s.Validate(), tc.field)
		})
	}
}

func TestFlowValidateNamesEveryBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Flow)
		field  string
	}{
		{"source_id is not a UUID", func(f *Flow) { f.SourceID = "src" }, "flow.source_id"},
		{"device_id is not a UUID", func(f *Flow) { f.DeviceID = "dev" }, "flow.device_id"},
		{"parents entry is not a UUID", func(f *Flow) { f.Parents = []string{"p"} }, "flow.parents[0]"},
		{"grain_rate numerator not positive", func(f *Flow) { f.GrainRate = &GrainRate{Numerator: -25} }, "flow.grain_rate.numerator"},
		{"format is an unknown x-nmos URN", func(f *Flow) { f.Format = "urn:x-nmos:format:hologram" }, "flow.format"},
		{"video frame_height missing", func(f *Flow) { f.FrameHeight = 0 }, "flow.frame_height"},
		{"video interlace_mode outside enum", func(f *Flow) { f.Interlace = "interlaced" }, "flow.interlace_mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := validFlow()
			tc.mutate(&f)
			wantValidation(t, f.Validate(), tc.field)
		})
	}
}

// TestFlowValidateIsLenientOnABareV10Body: a Flow with no per-format
// fields at all is what the v1.0 wire carries, and the canonical
// validator must not invent frame_width for it.
func TestFlowValidateIsLenientOnABareV10Body(t *testing.T) {
	f := validFlow()
	f.FrameWidth, f.FrameHeight, f.Interlace = 0, 0, ""
	if err := f.Validate(); err != nil {
		t.Fatalf("a v1.0-shaped video Flow must validate: %v", err)
	}
}

func TestSenderValidateNamesEveryBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Sender)
		field  string
	}{
		{"flow_id is not a UUID", func(s *Sender) { s.FlowID = strp("flow") }, "sender.flow_id"},
		{"device_id is not a UUID", func(s *Sender) { s.DeviceID = "dev" }, "sender.device_id"},
		{"manifest_href empty string", func(s *Sender) { s.ManifestHref = strp("") }, "sender.manifest_href"},
		{"subscription receiver_id is not a UUID", func(s *Sender) {
			s.Subscription.ReceiverID = strp("rcv")
		}, "sender.subscription.receiver_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSender()
			tc.mutate(&s)
			wantValidation(t, s.Validate(), tc.field)
		})
	}
}

func TestReceiverValidateNamesEveryBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Receiver)
		field  string
	}{
		{"device_id is not a UUID", func(r *Receiver) { r.DeviceID = "dev" }, "receiver.device_id"},
		{"transport is an unknown x-nmos URN", func(r *Receiver) { r.Transport = "urn:x-nmos:transport:teleport" }, "receiver.transport"},
		{"subscription sender_id is not a UUID", func(r *Receiver) {
			r.Subscription.SenderID = strp("snd")
		}, "receiver.subscription.sender_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validReceiver()
			tc.mutate(&r)
			wantValidation(t, r.Validate(), tc.field)
		})
	}
}

// decodeEntry is one canonical Decode* entry point with a fixture that
// passes it and a one-field mutation that fails it.
type decodeEntry struct {
	kind    string
	valid   func() any
	invalid func() any
	field   string
	decode  func([]byte) error
}

func decodeEntries() []decodeEntry {
	return []decodeEntry{
		{
			kind:    "node",
			valid:   func() any { n := validNode(); return n },
			invalid: func() any { n := validNode(); n.Href = ""; return n },
			field:   "node.href",
			decode:  func(b []byte) error { _, err := DecodeNode(b); return err },
		},
		{
			kind:    "device",
			valid:   func() any { d := validDevice(); return d },
			invalid: func() any { d := validDevice(); d.NodeID = "x"; return d },
			field:   "device.node_id",
			decode:  func(b []byte) error { _, err := DecodeDevice(b); return err },
		},
		{
			kind:    "source",
			valid:   func() any { s := validSource(); return s },
			invalid: func() any { s := validSource(); s.DeviceID = "x"; return s },
			field:   "source.device_id",
			decode:  func(b []byte) error { _, err := DecodeSource(b); return err },
		},
		{
			kind:    "flow",
			valid:   func() any { f := validFlow(); return f },
			invalid: func() any { f := validFlow(); f.SourceID = "x"; return f },
			field:   "flow.source_id",
			decode:  func(b []byte) error { _, err := DecodeFlow(b); return err },
		},
		{
			kind:    "sender",
			valid:   func() any { s := validSender(); return s },
			invalid: func() any { s := validSender(); s.Transport = ""; return s },
			field:   "sender.transport",
			decode:  func(b []byte) error { _, err := DecodeSender(b); return err },
		},
		{
			kind:    "receiver",
			valid:   func() any { r := validReceiver(); return r },
			invalid: func() any { r := validReceiver(); r.Format = ""; return r },
			field:   "receiver.format",
			decode:  func(b []byte) error { _, err := DecodeReceiver(b); return err },
		},
	}
}

// TestDecodeAcceptsEveryValidResource: parse + canonical validation on
// the happy path, for each of the six resource types.
func TestDecodeAcceptsEveryValidResource(t *testing.T) {
	for _, e := range decodeEntries() {
		t.Run(e.kind, func(t *testing.T) {
			raw, err := json.Marshal(e.valid())
			if err != nil {
				t.Fatal(err)
			}
			if err := e.decode(raw); err != nil {
				t.Fatalf("a valid %s must decode: %v", e.kind, err)
			}
		})
	}
}

// TestDecodeValidatesAfterParse: a body that parses but breaks a
// canonical rule is refused with the field named. Parsing is lenient;
// validation is not.
func TestDecodeValidatesAfterParse(t *testing.T) {
	for _, e := range decodeEntries() {
		t.Run(e.kind, func(t *testing.T) {
			raw, err := json.Marshal(e.invalid())
			if err != nil {
				t.Fatal(err)
			}
			wantValidation(t, e.decode(raw), e.field)
		})
	}
}

// TestDecodeRefusesMalformedJSON: absorbing unknown fields does not
// extend to unreadable input, on any resource type.
func TestDecodeRefusesMalformedJSON(t *testing.T) {
	for _, e := range decodeEntries() {
		t.Run(e.kind, func(t *testing.T) {
			err := e.decode([]byte(`{"id":`))
			if err == nil || !strings.Contains(err.Error(), "decode "+e.kind) {
				t.Fatalf("want a decode error naming the %s, got %v", e.kind, err)
			}
		})
	}
}
