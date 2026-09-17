package v10

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/jsonschema"
	"dhs/internal/amwa/codec/spec"
)

const (
	nodeID     = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	deviceID   = "12345678-1234-4abc-9def-1234567890ab"
	sourceID   = "11111111-1111-4111-8111-111111111111"
	flowID     = "22222222-2222-4222-8222-222222222222"
	senderID   = "44444444-4444-4444-8444-444444444444"
	receiverID = "66666666-6666-4666-8666-666666666666"
)

func TestNewIsTheZeroCodec(t *testing.T) {
	if New() != (Codec{}) {
		t.Fatal("New must be equivalent to Codec{}")
	}
}

// TestEveryResourceRoundTripsOnTheWire: encode, then decode our own
// bytes with a reporter attached. A body we emitted must come back with
// the same identity and provoke no deviation, or the encoder and the
// decoder disagree about what v1.0.3 is.
func TestEveryResourceRoundTripsOnTheWire(t *testing.T) {
	rep := &spec.SliceReporter{}
	c := Codec{Reporter: rep}
	cases := []struct {
		kind string
		want string
		run  func() (string, error)
	}{
		{"device", deviceID, func() (string, error) {
			b, err := c.EncodeDevice(validDeviceV10(deviceID))
			if err != nil {
				return "", err
			}
			got, err := c.DecodeDevice(b)
			return got.ID, err
		}},
		{"source", sourceID, func() (string, error) {
			b, err := c.EncodeSource(validSourceV10(sourceID))
			if err != nil {
				return "", err
			}
			got, err := c.DecodeSource(b)
			return got.ID, err
		}},
		{"flow", flowID, func() (string, error) {
			b, err := c.EncodeFlow(validFlowV10(flowID))
			if err != nil {
				return "", err
			}
			got, err := c.DecodeFlow(b)
			return got.ID, err
		}},
		{"sender", senderID, func() (string, error) {
			b, err := c.EncodeSender(validSenderV10(senderID))
			if err != nil {
				return "", err
			}
			got, err := c.DecodeSender(b)
			return got.ID, err
		}},
		{"receiver", receiverID, func() (string, error) {
			b, err := c.EncodeReceiver(validReceiverV10(receiverID))
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

func TestValidateAcceptsEveryFixture(t *testing.T) {
	c := Codec{}
	checks := map[string]error{
		"node":     c.ValidateNode(validNodeV10(nodeID)),
		"device":   c.ValidateDevice(validDeviceV10(deviceID)),
		"source":   c.ValidateSource(validSourceV10(sourceID)),
		"flow":     c.ValidateFlow(validFlowV10(flowID)),
		"sender":   c.ValidateSender(validSenderV10(senderID)),
		"receiver": c.ValidateReceiver(validReceiverV10(receiverID)),
	}
	for kind, err := range checks {
		if err != nil {
			t.Errorf("%s: AMWA's v1.0.3 schema must accept the fixture: %v", kind, err)
		}
	}
}

// TestValidateRejectsANonUUIDIdentifier: the refusal is AMWA's own
// schema speaking (a typed ValidationError), for every resource kind.
func TestValidateRejectsANonUUIDIdentifier(t *testing.T) {
	c := Codec{}
	checks := map[string]error{
		"node":     c.ValidateNode(validNodeV10("x")),
		"device":   c.ValidateDevice(validDeviceV10("x")),
		"source":   c.ValidateSource(validSourceV10("x")),
		"flow":     c.ValidateFlow(validFlowV10("x")),
		"sender":   c.ValidateSender(validSenderV10("x")),
		"receiver": c.ValidateReceiver(validReceiverV10("x")),
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
	n := validNodeV10(nodeID)
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

	_, err := Codec{}.EncodeNode(validNodeV10(nodeID))
	if err == nil || !strings.Contains(err.Error(), "strip") {
		t.Fatalf("want the strip stage's error, got %v", err)
	}
}

// TestEncodeReportsWhatDropEmptyCannotRender: the second seam-only
// branch. The strip stage succeeds on real bytes, then dropEmpty's own
// render fails; encode must hand that failure back.
func TestEncodeReportsWhatDropEmptyCannotRender(t *testing.T) {
	boom := errors.New("render refused")
	calls := 0
	old := marshal
	marshal = func(v any) ([]byte, error) {
		calls++
		if calls == 1 {
			return json.Marshal(v)
		}
		return nil, boom
	}
	t.Cleanup(func() { marshal = old })

	_, err := Codec{}.EncodeNode(validNodeV10(nodeID))
	if !errors.Is(err, boom) {
		t.Fatalf("want dropEmpty's failure surfaced, got %v", err)
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
