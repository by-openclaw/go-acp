package v10

import (
	"testing"

	"dhs/internal/amwa/codec/is08"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// The v1.0 codec is a thin delegation to the canonical rules — IS-08
// has only ever had one minor — so what this pins is that every
// method is wired to its own canonical function and that the codec is
// registered under the version it claims.
func TestV10CodecDelegatesEveryBody(t *testing.T) {
	c := New()
	if c.SpecID() != is08.SpecID || c.APIVer() != "v1.0" || c.SpecPatch() != SpecPatch {
		t.Errorf("identity = %q %q %q", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	if got, ok := is08.Get("v1.0"); !ok || got.APIVer() != "v1.0" {
		t.Errorf("the codec must register itself: %v, %v", got, ok)
	}

	entries := is08.MapEntries{
		"out-1": {"0": {Input: strPtr("in-1"), ChannelIndex: intPtr(0)}},
	}
	mode := is08.ActivationModeImmediate

	active := is08.MapActive{Activation: is08.ActivationResponse{Mode: &mode}, Map: entries}
	if err := c.ValidateMapActive(active); err != nil {
		t.Fatalf("ValidateMapActive: %v", err)
	}
	raw, err := c.EncodeMapActive(active)
	if err != nil {
		t.Fatalf("EncodeMapActive: %v", err)
	}
	if back, err := c.DecodeMapActive(raw); err != nil || len(back.Map) != 1 {
		t.Errorf("DecodeMapActive = %+v, %v", back, err)
	}

	req := is08.MapActivationRequest{
		Activation: is08.Activation{Mode: is08.ActivationModeImmediate}, Action: entries,
	}
	if err := c.ValidateMapActivationRequest(req); err != nil {
		t.Fatalf("ValidateMapActivationRequest: %v", err)
	}
	raw, err = c.EncodeMapActivationRequest(req)
	if err != nil {
		t.Fatalf("EncodeMapActivationRequest: %v", err)
	}
	if back, err := c.DecodeMapActivationRequest(raw); err != nil || len(back.Action) != 1 {
		t.Errorf("DecodeMapActivationRequest = %+v, %v", back, err)
	}

	io := is08.IO{
		Inputs:  map[string]is08.Input{"in-1": {Channels: []is08.Channel{{Label: "L"}}}},
		Outputs: map[string]is08.Output{"out-1": {Channels: []is08.Channel{{Label: "L"}}}},
	}
	if err := c.ValidateIO(io); err != nil {
		t.Fatalf("ValidateIO: %v", err)
	}
	raw, err = c.EncodeIO(io)
	if err != nil {
		t.Fatalf("EncodeIO: %v", err)
	}
	if back, err := c.DecodeIO(raw); err != nil || len(back.Inputs) != 1 {
		t.Errorf("DecodeIO = %+v, %v", back, err)
	}

	// Each refusal reaches the caller through the codec too.
	if err := c.ValidateIO(is08.IO{}); err == nil {
		t.Error("ValidateIO accepted a body with no sections")
	}
	if _, err := c.DecodeIO([]byte(`{`)); err == nil {
		t.Error("DecodeIO accepted malformed JSON")
	}
	if _, err := c.DecodeMapActive([]byte(`{`)); err == nil {
		t.Error("DecodeMapActive accepted malformed JSON")
	}
	if err := c.ValidateMapActive(is08.MapActive{}); err == nil {
		t.Error("ValidateMapActive accepted a body with no map")
	}
	if err := c.ValidateMapActivationRequest(is08.MapActivationRequest{}); err == nil {
		t.Error("ValidateMapActivationRequest accepted a body with no mode")
	}
}
