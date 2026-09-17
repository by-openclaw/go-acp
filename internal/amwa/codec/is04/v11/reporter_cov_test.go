package v11

import "testing"

// A codec with no reporter still decodes: the deviation check is what
// a compliance run wires up, not a decode requirement, so its absence
// must cost nothing.
func TestDecodeWithoutAReporter(t *testing.T) {
	// A body that deviates from the v1.1 schema (no `label`), which a
	// reporter-carrying codec would report on.
	raw := []byte(`{"id":"f47ac10b-58cc-4372-a567-0e02b2c3d479","version":"0:0",` +
		`"description":"d","tags":{},"type":"urn:x-nmos:device:generic",` +
		`"node_id":"f47ac10b-58cc-4372-a567-0e02b2c3d479","senders":[],"receivers":[],"controls":[]}`)
	if _, err := (Codec{}).DecodeDevice(raw); err != nil {
		t.Fatalf("DecodeDevice with no reporter: %v", err)
	}
}
