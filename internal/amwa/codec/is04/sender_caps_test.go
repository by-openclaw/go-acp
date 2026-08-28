package is04

import (
	"encoding/json"
	"strings"
	"testing"
)

// A sender registered WITH `"caps": {}` must re-encode with it, and a
// sender registered WITHOUT caps must not grow one. A real EVS Neuron
// registers all 208 senders with the empty object, and map+omitempty
// silently dropped every one on the Registry's way back out — nothing
// failed (caps is optional), the document just stopped being the one
// that was registered.
func TestSenderCapsAbsentVsEmptySurviveRoundTrip(t *testing.T) {
	withCaps := []byte(`{"id":"4903e656-8186-4a9e-b270-ee76704181b4","caps":{},"transport":"urn:x-nmos:transport:rtp.mcast"}`)
	var s Sender
	if err := json.Unmarshal(withCaps, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Caps == nil {
		t.Fatal("decode lost the empty caps object")
	}
	out, _ := json.Marshal(s)
	if !strings.Contains(string(out), `"caps":{}`) {
		t.Errorf("re-encode dropped the registered empty caps: %s", out)
	}

	var s2 Sender
	if err := json.Unmarshal([]byte(`{"id":"4903e656-8186-4a9e-b270-ee76704181b4"}`), &s2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s2.Caps != nil {
		t.Fatal("absent caps must decode to nil")
	}
	out2, _ := json.Marshal(s2)
	if strings.Contains(string(out2), "caps") {
		t.Errorf("re-encode invented a caps field: %s", out2)
	}
}
