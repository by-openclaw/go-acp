package v11

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
		Description: "v1.1 fixture",
		Tags:        map[string][]string{},
	}
}

// The fixtures below are the minimum each v1.1.3 schema accepts.
// Encode is schema-checked, so a passing encode IS AMWA's own schema
// accepting the shape; the tests never restate a rule by hand.

func fixtureDevice() is04.Device {
	return is04.Device{
		ResourceCore: core(deviceID, "v1.1 Device"),
		Type:         "urn:x-nmos:device:generic",
		NodeID:       nodeID,
		Senders:      []string{senderID},
		Receivers:    []string{receiverID},
		Controls: []is04.DeviceControl{{
			Href:          "http://dhs.local:8080/x-nmos/connection/",
			Type:          "urn:x-nmos:control:sr-ctrl/v1.0",
			Authorization: true,
		}},
	}
}

func fixtureSource() is04.Source {
	clk := "clk0"
	return is04.Source{
		ResourceCore: core(sourceID, "v1.1 Source"),
		Caps:         map[string]any{},
		DeviceID:     deviceID,
		Parents:      []string{},
		ClockName:    &clk,
		Format:       is04.FormatVideo,
	}
}

func fixtureFlow() is04.Flow {
	return is04.Flow{
		ResourceCore: core(flowID, "v1.1 Flow"),
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

// fixtureReceiver leaves interface_bindings unset: v1.1 has no such
// property and the encoder strips it.
func fixtureReceiver() is04.Receiver {
	return is04.Receiver{
		ResourceCore: core(receiverID, "v1.1 Receiver"),
		DeviceID:     deviceID,
		Transport:    is04.TransportRTPMcast,
		Format:       is04.FormatVideo,
		Caps:         is04.ReceiverCaps{MediaTypes: []string{"video/raw"}},
		Subscription: is04.ReceiverSubscription{},
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
// decoder disagree about what v1.1.3 is.
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
		{"receiver", receiverID, func() (string, error) {
			b, err := c.EncodeReceiver(fixtureReceiver())
			if err != nil {
				return "", err
			}
			got, err := c.DecodeReceiver(b)
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

// TestV11WireDropsLaterProperties: controls[].authorization (v1.3),
// receiver interface_bindings (v1.2) and receiver subscription.active
// (v1.2) must not appear on a v1.1 body, while subscription.sender_id,
// which v1.1 requires, must.
func TestV11WireDropsLaterProperties(t *testing.T) {
	c := Codec{}
	body, err := c.EncodeDevice(fixtureDevice())
	if err != nil {
		t.Fatalf("Encode device: %v", err)
	}
	if strings.Contains(string(body), "authorization") {
		t.Errorf("v1.1 wire must not carry controls[].authorization: %s", body)
	}
	r := fixtureReceiver()
	r.InterfaceBindings = []string{"eth0"}
	r.Subscription.Active = true
	body, err = c.EncodeReceiver(r)
	if err != nil {
		t.Fatalf("Encode receiver: %v", err)
	}
	for _, gone := range []string{"interface_bindings", `"active"`} {
		if strings.Contains(string(body), gone) {
			t.Errorf("v1.1 wire must not carry %s: %s", gone, body)
		}
	}
	if !strings.Contains(string(body), `"sender_id": null`) {
		t.Errorf("v1.1 requires subscription.sender_id, null when unrouted: %s", body)
	}
}

func TestValidateAcceptsEveryFixture(t *testing.T) {
	c := Codec{}
	checks := map[string]error{
		"node":     c.ValidateNode(validNodeV11(nodeID)),
		"device":   c.ValidateDevice(fixtureDevice()),
		"source":   c.ValidateSource(fixtureSource()),
		"flow":     c.ValidateFlow(fixtureFlow()),
		"sender":   c.ValidateSender(validSenderV11(senderID)),
		"receiver": c.ValidateReceiver(fixtureReceiver()),
	}
	for kind, err := range checks {
		if err != nil {
			t.Errorf("%s: AMWA's v1.1.3 schema must accept the fixture: %v", kind, err)
		}
	}
}

// TestValidateRejectsANonUUIDIdentifier: the refusal is AMWA's own
// schema speaking (a typed ValidationError), for every resource kind.
func TestValidateRejectsANonUUIDIdentifier(t *testing.T) {
	c := Codec{}
	d, s, f, rcv := fixtureDevice(), fixtureSource(), fixtureFlow(), fixtureReceiver()
	d.ID, s.ID, f.ID, rcv.ID = "x", "x", "x", "x"
	checks := map[string]error{
		"node":     c.ValidateNode(validNodeV11("x")),
		"device":   c.ValidateDevice(d),
		"source":   c.ValidateSource(s),
		"flow":     c.ValidateFlow(f),
		"sender":   c.ValidateSender(validSenderV11("x")),
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
	n := validNodeV11(nodeID)
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

	_, err := Codec{}.EncodeNode(validNodeV11(nodeID))
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
