package v12

import (
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
		Description: "v1.2 fixture",
		Tags:        map[string][]string{},
	}
}

// The fixtures below are the minimum each v1.2.2 schema accepts.
// Encode is schema-checked, so a passing encode IS AMWA's own schema
// accepting the shape; the tests never restate a rule by hand.

func fixtureDevice() is04.Device {
	return is04.Device{
		ResourceCore: core(deviceID, "v1.2 Device"),
		Type:         "urn:x-nmos:device:generic",
		NodeID:       nodeID,
		Senders:      []string{senderID},
		Receivers:    []string{receiverID},
		Controls: []is04.DeviceControl{{
			Href:          "http://dhs.local:8080/x-nmos/connection/",
			Type:          "urn:x-nmos:control:sr-ctrl/v1.1",
			Authorization: true,
		}},
	}
}

func fixtureSource() is04.Source {
	clk := "clk0"
	return is04.Source{
		ResourceCore: core(sourceID, "v1.2 Source"),
		Caps:         map[string]any{},
		DeviceID:     deviceID,
		Parents:      []string{},
		ClockName:    &clk,
		Format:       is04.FormatVideo,
	}
}

func fixtureFlow() is04.Flow {
	return is04.Flow{
		ResourceCore: core(flowID, "v1.2 Flow"),
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
		ResourceCore:      core(senderID, "v1.2 Sender"),
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
		ResourceCore:      core(receiverID, "v1.2 Receiver"),
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
// decoder disagree about what v1.2.2 is.
func TestEveryResourceRoundTripsOnTheWire(t *testing.T) {
	rep := &spec.SliceReporter{}
	c := Codec{Reporter: rep}
	cases := []struct {
		kind string
		want string
		run  func() (string, error)
	}{
		{"device", deviceID, func() (string, error) {
			b, err := c.EncodeDevice(fixtureDevice())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeDevice(b)
			return got.ID, err
		}},
		{"source", sourceID, func() (string, error) {
			b, err := c.EncodeSource(fixtureSource())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeSource(b)
			return got.ID, err
		}},
		{"flow", flowID, func() (string, error) {
			b, err := c.EncodeFlow(fixtureFlow())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeFlow(b)
			return got.ID, err
		}},
		{"sender", senderID, func() (string, error) {
			b, err := c.EncodeSender(fixtureSender())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeSender(b)
			return got.ID, err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			id, err := tc.run()
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			if id != tc.want {
				t.Fatalf("id = %q, want %q", id, tc.want)
			}
			if n := len(rep.Snapshot()); n != 0 {
				t.Fatalf("our own %s provoked %d deviations: %v", tc.kind, n, rep.Snapshot())
			}
		})
	}
}

// TestDeviceEncodeStripsControlAuthorization: `authorization` on a
// control is a v1.3 property; the v1.2 wire must not carry it.
func TestDeviceEncodeStripsControlAuthorization(t *testing.T) {
	body, err := Codec{}.EncodeDevice(fixtureDevice())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(body), "authorization") {
		t.Fatalf("v1.2 wire must not carry controls[].authorization: %s", body)
	}
}

// TestSenderManifestHrefMustBeAString: v1.2.2 sender.json types
// manifest_href as a plain string; null only becomes legal in v1.3, so
// a Sender without a transport file cannot be described on this wire.
func TestSenderManifestHrefMustBeAString(t *testing.T) {
	s := fixtureSender()
	s.ManifestHref = nil
	_, err := Codec{}.EncodeSender(s)
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("v1.2 must reject a null manifest_href with the schema's own error, got %v", err)
	}
}

// TestReceiverRequiresInterfaceBindings: interface_bindings arrives in
// v1.2 as a REQUIRED array; a Receiver that leaves it unset is refused
// rather than emitted with null.
func TestReceiverRequiresInterfaceBindings(t *testing.T) {
	r := fixtureReceiver()
	r.InterfaceBindings = nil
	_, err := Codec{}.EncodeReceiver(r)
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("v1.2 must reject a Receiver without interface_bindings, got %v", err)
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
			t.Errorf("%s: AMWA's v1.2.2 schema must accept the fixture: %v", kind, err)
		}
	}
}

// TestValidateRejectsANonUUIDIdentifier: the refusal is AMWA's own
// schema speaking (a typed ValidationError), for every resource kind.
func TestValidateRejectsANonUUIDIdentifier(t *testing.T) {
	c := Codec{}
	n, d, s, f, snd, rcv := validNode("x"), fixtureDevice(), fixtureSource(), fixtureFlow(), fixtureSender(), fixtureReceiver()
	d.ID, s.ID, f.ID, snd.ID, rcv.ID = "x", "x", "x", "x", "x"
	checks := map[string]error{
		"node":     c.ValidateNode(n),
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

// TestEncodeReportsWhatTheStripStageRefuses exercises the branch no
// input reaches (see marshal): bytes the marshaller hands back that the
// strip stage cannot read must surface as an error, not as a payload.
func TestEncodeReportsWhatTheStripStageRefuses(t *testing.T) {
	old := marshal
	marshal = func(any) ([]byte, error) { return []byte(`{`), nil }
	t.Cleanup(func() { marshal = old })

	_, err := Codec{}.EncodeNode(validNode(nodeID))
	if err == nil || !strings.Contains(err.Error(), "strip") {
		t.Fatalf("want the strip stage's error, got %v", err)
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
