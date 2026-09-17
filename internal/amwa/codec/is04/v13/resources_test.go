package v13

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/jsonschema"
	"dhs/internal/amwa/codec/spec"
)

const (
	nodeID     = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	deviceID   = "abcdef01-1234-4abc-9def-1234567890ab"
	sourceID   = "11111111-1111-4111-8111-111111111111"
	flowID     = "22222222-2222-4222-8222-222222222222"
	senderID   = "33333333-3333-4333-8333-333333333333"
	receiverID = "44444444-4444-4444-8444-444444444444"
)

func core(id, label string) is04.ResourceCore {
	return is04.ResourceCore{
		ID:          id,
		Version:     "1700000000:0",
		Label:       label,
		Description: "v1.3 fixture",
		Tags:        map[string][]string{},
	}
}

// The fixtures below are the minimum each v1.3.3 schema accepts.
// Encode is schema-checked, so a passing encode IS AMWA's own schema
// accepting the shape; the tests never restate a rule by hand.

func fixtureDevice() is04.Device {
	return is04.Device{
		ResourceCore: core(deviceID, "v1.3 Device"),
		Type:         "urn:x-nmos:device:generic",
		NodeID:       nodeID,
		Senders:      []string{senderID},
		Receivers:    []string{receiverID},
		Controls: []is04.DeviceControl{{
			Href:          "http://dhs.local:8080/x-nmos/connection/",
			Type:          "urn:x-nmos:control:sr-ctrl/v1.1",
			Authorization: false,
		}},
	}
}

func fixtureSource() is04.Source {
	clk := "clk0"
	return is04.Source{
		ResourceCore: core(sourceID, "v1.3 Source"),
		Caps:         map[string]any{},
		DeviceID:     deviceID,
		Parents:      []string{},
		ClockName:    &clk,
		Format:       is04.FormatVideo,
	}
}

func fixtureFlow() is04.Flow {
	return is04.Flow{
		ResourceCore: core(flowID, "v1.3 Flow"),
		SourceID:     sourceID,
		DeviceID:     deviceID,
		Parents:      []string{},
		Format:       is04.FormatVideo,
		MediaType:    "video/raw",
		FrameWidth:   1920,
		FrameHeight:  1080,
		Interlace:    "progressive",
		ColorSpace:   "BT709",
		Components: []is04.FlowVideoComponent{
			{Name: "Y", Width: 1920, Height: 1080, BitDepth: 10},
			{Name: "Cb", Width: 960, Height: 1080, BitDepth: 10},
			{Name: "Cr", Width: 960, Height: 1080, BitDepth: 10},
		},
	}
}

func fixtureSender() is04.Sender {
	flow := flowID
	manifest := "http://dhs.local:8080/x-nmos/connection/v1.1/single/senders/" + senderID + "/transportfile"
	return is04.Sender{
		ResourceCore:      core(senderID, "v1.3 Sender"),
		FlowID:            &flow,
		Transport:         is04.TransportRTPMcast,
		DeviceID:          deviceID,
		ManifestHref:      &manifest,
		InterfaceBindings: []string{"eth0"},
		Subscription:      is04.SenderSubscription{},
	}
}

func fixtureReceiver() is04.Receiver {
	return is04.Receiver{
		ResourceCore:      core(receiverID, "v1.3 Receiver"),
		DeviceID:          deviceID,
		Transport:         is04.TransportRTPMcast,
		InterfaceBindings: []string{"eth0"},
		Format:            is04.FormatVideo,
		Caps:              is04.ReceiverCaps{MediaTypes: []string{"video/raw"}},
		Subscription:      is04.ReceiverSubscription{},
	}
}

func TestNewIsTheZeroCodec(t *testing.T) {
	if New() != (Codec{}) {
		t.Fatal("New must be equivalent to Codec{}")
	}
}

// TestEveryResourceRoundTripsOnTheWire: encode, then decode our own
// bytes with a reporter attached. A body we emitted must come back with
// the same identity and provoke no deviation, or the encoder and the
// decoder disagree about what v1.3.3 is.
func TestEveryResourceRoundTripsOnTheWire(t *testing.T) {
	rep := &spec.SliceReporter{}
	c := Codec{Reporter: rep}
	cases := []struct {
		kind string
		run  func() (string, error)
	}{
		{"device", func() (string, error) {
			b, err := c.EncodeDevice(fixtureDevice())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeDevice(b)
			return got.ID, err
		}},
		{"source", func() (string, error) {
			b, err := c.EncodeSource(fixtureSource())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeSource(b)
			return got.ID, err
		}},
		{"flow", func() (string, error) {
			b, err := c.EncodeFlow(fixtureFlow())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeFlow(b)
			return got.ID, err
		}},
		{"sender", func() (string, error) {
			b, err := c.EncodeSender(fixtureSender())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeSender(b)
			return got.ID, err
		}},
		{"receiver", func() (string, error) {
			b, err := c.EncodeReceiver(fixtureReceiver())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeReceiver(b)
			return got.ID, err
		}},
	}
	want := map[string]string{
		"device": deviceID, "source": sourceID, "flow": flowID,
		"sender": senderID, "receiver": receiverID,
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			id, err := tc.run()
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			if id != want[tc.kind] {
				t.Fatalf("id = %q, want %q", id, want[tc.kind])
			}
			if n := len(rep.Snapshot()); n != 0 {
				t.Fatalf("our own %s provoked %d deviations: %v", tc.kind, n, rep.Snapshot())
			}
		})
	}
}

// TestV13KeepsWhatEarlierMinorsDrop: `authorization` on services,
// endpoints and controls, and interfaces[].attached_network_device,
// all arrive in v1.3 and must survive onto this wire.
func TestV13KeepsWhatEarlierMinorsDrop(t *testing.T) {
	n := validNode(nodeID)
	n.Services = []is04.NodeService{{Href: "http://dhs.local/svc", Type: "urn:x-vendor:service:x", Authorization: true}}
	n.API.Endpoints[0].Authorization = true
	n.Interfaces[0].AttachedNetworkDevice = &is04.AttachedNetworkDevice{ChassisID: "00-11-22-33-44-55", PortID: "Eth1/1"}
	body, err := Codec{}.EncodeNode(n)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, key := range []string{`"authorization": true`, `"attached_network_device"`} {
		if !strings.Contains(string(body), key) {
			t.Errorf("v1.3 wire must carry %s: %s", key, body)
		}
	}
	d := fixtureDevice()
	body, err = Codec{}.EncodeDevice(d)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(body), `"authorization": false`) {
		t.Errorf("v1.3 wire must carry controls[].authorization even when false: %s", body)
	}
}

// TestSenderManifestHrefMayBeNull: v1.3.3 sender.json types
// manifest_href ["string", "null"]; a Sender with no transport file
// says so with null, and this minor accepts it.
func TestSenderManifestHrefMayBeNull(t *testing.T) {
	s := fixtureSender()
	s.ManifestHref = nil
	body, err := Codec{}.EncodeSender(s)
	if err != nil {
		t.Fatalf("v1.3 must accept a null manifest_href: %v", err)
	}
	if !strings.Contains(string(body), `"manifest_href": null`) {
		t.Fatalf("null must reach the wire, not be omitted: %s", body)
	}
}

func TestValidateAcceptsEveryFixture(t *testing.T) {
	c := Codec{}
	checks := map[string]error{
		"node":     c.ValidateNode(validNode(nodeID)),
		"device":   c.ValidateDevice(fixtureDevice()),
		"source":   c.ValidateSource(fixtureSource()),
		"flow":     c.ValidateFlow(fixtureFlow()),
		"sender":   c.ValidateSender(fixtureSender()),
		"receiver": c.ValidateReceiver(fixtureReceiver()),
	}
	for kind, err := range checks {
		if err != nil {
			t.Errorf("%s: AMWA's v1.3.3 schema must accept the fixture: %v", kind, err)
		}
	}
}

// TestValidateRejectsANonUUIDIdentifier: the refusal is AMWA's own
// schema speaking (a typed ValidationError), for every resource kind.
func TestValidateRejectsANonUUIDIdentifier(t *testing.T) {
	c := Codec{}
	d, s, f, snd, rcv := fixtureDevice(), fixtureSource(), fixtureFlow(), fixtureSender(), fixtureReceiver()
	d.ID, s.ID, f.ID, snd.ID, rcv.ID = "x", "x", "x", "x", "x"
	checks := map[string]error{
		"device":   c.ValidateDevice(d),
		"source":   c.ValidateSource(s),
		"flow":     c.ValidateFlow(f),
		"sender":   c.ValidateSender(snd),
		"receiver": c.ValidateReceiver(rcv),
	}
	for kind, err := range checks {
		var ve *jsonschema.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: want AMWA's schema to reject id %q, got %v", kind, "x", err)
		}
	}
}

// TestDecodeRefusesWhatIsNotJSON: tolerance stops at unreadable input;
// the caller gets the zero resource and an error, never a half-filled
// struct.
func TestDecodeRefusesWhatIsNotJSON(t *testing.T) {
	c := Codec{}
	raw := []byte(`{"id":`)
	if got, err := c.DecodeNode(raw); err == nil || got.ID != "" {
		t.Errorf("node: got %+v, %v", got, err)
	}
	if got, err := c.DecodeDevice(raw); err == nil || got.ID != "" {
		t.Errorf("device: got %+v, %v", got, err)
	}
	if got, err := c.DecodeSource(raw); err == nil || got.ID != "" {
		t.Errorf("source: got %+v, %v", got, err)
	}
	if got, err := c.DecodeFlow(raw); err == nil || got.ID != "" {
		t.Errorf("flow: got %+v, %v", got, err)
	}
	if got, err := c.DecodeSender(raw); err == nil || got.ID != "" {
		t.Errorf("sender: got %+v, %v", got, err)
	}
	if got, err := c.DecodeReceiver(raw); err == nil || got.ID != "" {
		t.Errorf("receiver: got %+v, %v", got, err)
	}
}

// TestEncodeRefusesAValueItCannotMarshal: caps is free-form, so a
// caller can put something unserialisable in it; the error names the
// stage and the resource.
func TestEncodeRefusesAValueItCannotMarshal(t *testing.T) {
	n := validNode(nodeID)
	n.Caps = map[string]any{"poison": make(chan int)}
	_, err := Codec{}.EncodeNode(n)
	if err == nil || !strings.Contains(err.Error(), "marshal node") {
		t.Fatalf("want a marshal error naming the node, got %v", err)
	}
}

// TestReportDeviationsNamesALoaderFailure: when the schema itself
// cannot be loaded there are no per-field problems to list, and the
// single event must carry the loader's own words.
func TestReportDeviationsNamesALoaderFailure(t *testing.T) {
	rep := &spec.SliceReporter{}
	Codec{Reporter: rep}.reportDeviations("hologram", []byte(`{}`))
	events := rep.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Code != "nmos_is04_schema_deviation" || !strings.Contains(events[0].Detail, "hologram.json") {
		t.Fatalf("event = %+v", events[0])
	}
}

// TestSchemaDeviationIsReportedNotFatal: a payload AMWA's own schema
// rejects is still returned to the caller, but every failure is named
// as a compliance event so nothing is swallowed.
func TestSchemaDeviationIsReportedNotFatal(t *testing.T) {
	bad := []byte(`{"id":"not-a-uuid","version":"whenever","label":"x","description":"","tags":{},"node_id":"22222222-2222-4222-8222-222222222222","type":"urn:x-nmos:device:generic","senders":[],"receivers":[],"controls":[]}`)
	rep := &spec.SliceReporter{}
	d, err := (Codec{Reporter: rep}).DecodeDevice(bad)
	if err != nil {
		t.Fatalf("a schema deviation must not stop the decode: %v", err)
	}
	if d.ID != "not-a-uuid" {
		t.Fatalf("the resource must still reach the caller, got %+v", d)
	}
	events := rep.Snapshot()
	if len(events) < 2 {
		t.Fatalf("id and version each break a stated rule; got %d events: %v", len(events), events)
	}
	for _, e := range events {
		if e.Code != "nmos_is04_schema_deviation" || e.APIVer != "v1.3" || e.SpecPatch != "v1.3.3" {
			t.Errorf("event = %+v", e)
		}
	}
	var m map[string]any
	if err := json.Unmarshal(bad, &m); err != nil {
		t.Fatal(err)
	}
}
