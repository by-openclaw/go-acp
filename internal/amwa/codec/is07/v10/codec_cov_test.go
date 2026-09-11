package v10

import (
	"testing"

	"dhs/internal/amwa/codec/is07"
)

// The receiver → sender half of the v1.0 codec: both command
// envelopes go out and come back through the codec, and a command it
// cannot ship is refused rather than emitted.
func TestV10CodecCommands(t *testing.T) {
	c := New()
	const sourceID = "11111111-1111-4111-8111-111111111111"

	for name, cmd := range map[string]is07.Command{
		"a health probe":        is07.CommandHealth{Timestamp: "1600000000:0"},
		"a subscription":        is07.CommandSubscription{Sources: []string{sourceID}},
		"an empty subscription": is07.CommandSubscription{Sources: []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := c.EncodeCommand(cmd)
			if err != nil {
				t.Fatalf("EncodeCommand: %v", err)
			}
			back, err := c.DecodeCommand(raw)
			if err != nil {
				t.Fatalf("DecodeCommand: %v (%s)", err, raw)
			}
			if back.Kind() != cmd.Kind() {
				t.Errorf("round-tripped kind = %q, want %q", back.Kind(), cmd.Kind())
			}
		})
	}

	if _, err := c.EncodeCommand(is07.CommandHealth{}); err == nil {
		t.Error("a health command with no timestamp was encoded")
	}
	if _, err := c.DecodeCommand([]byte(`{`)); err == nil {
		t.Error("DecodeCommand accepted a frame that is not JSON")
	}
	if _, err := c.DecodeCommand([]byte(`{"command":"reset"}`)); err == nil {
		t.Error("DecodeCommand accepted a command the spec does not define")
	}
}
