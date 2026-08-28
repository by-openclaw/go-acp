package is04

import (
	"encoding/json"
	"testing"
)

// These are the WIRE SHAPES a real EVS Neuron registers — captured
// live 2026-08-28 — reduced to the fields that were each, at some
// point this session, silently lost by a typed round-trip. One test
// per resource, all three in one file, so the next omitempty-style
// regression on ANY of them fails with the device's name on it.
//
// The rule under test: what a peer registers is what the Registry
// re-serves. Field-presence fidelity is not cosmetic — Cerebrum's
// panels went blank on the dropped control field with no error
// anywhere.

func roundTripKeys(t *testing.T, wire []byte, v any) map[string]any {
	t.Helper()
	if err := json.Unmarshal(wire, v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	return got
}

func TestNeuronShapedNodeRoundTrips(t *testing.T) {
	wire := []byte(`{
	 "id":"b7011c4e-5f39-5a1a-a6eb-a8036b0a5fd9","version":"1787876573:0",
	 "label":"bm-n-nnbrg-c01","description":"","tags":{},
	 "href":"http://10.6.255.102:3000/","caps":{},
	 "api":{"versions":["v1.0","v1.1","v1.2","v1.3"],
	  "endpoints":[{"host":"10.6.255.102","port":3000,"protocol":"http","authorization":false}]},
	 "services":[],
	 "clocks":[{"name":"clk0","ref_type":"internal"},
	  {"name":"clk1","ref_type":"ptp","traceable":false,"version":"IEEE1588-2008","gmid":"e8-ea-6a-ff-fe-09-bb-9e","locked":false}],
	 "interfaces":[{"chassis_id":null,"port_id":"00-03-41-20-b4-60","name":"en8"}]}`)
	var n Node
	got := roundTripKeys(t, wire, &n)

	clocks := got["clocks"].([]any)
	ptp := clocks[1].(map[string]any)
	for _, k := range []string{"traceable", "version", "gmid", "locked"} {
		if _, ok := ptp[k]; !ok {
			t.Errorf("ptp clock lost required %q on round-trip", k)
		}
	}
	internal := clocks[0].(map[string]any)
	if len(internal) != 2 {
		t.Errorf("internal clock grew fields: %v", internal)
	}
	ep := got["api"].(map[string]any)["endpoints"].([]any)[0].(map[string]any)
	if _, ok := ep["authorization"]; !ok {
		t.Error("endpoint lost authorization on round-trip")
	}
}

func TestNeuronShapedDeviceRoundTrips(t *testing.T) {
	wire := []byte(`{
	 "id":"3a513f69-6543-54f4-b3ca-6625f3377418","version":"1787876573:0",
	 "label":"bm-n-nnbrg-c01","description":"","tags":{},
	 "type":"urn:x-nmos:device:generic",
	 "node_id":"b7011c4e-5f39-5a1a-a6eb-a8036b0a5fd9",
	 "senders":[],"receivers":[],
	 "controls":[
	  {"href":"http://10.6.255.102:3000/x-nmos/connection/v1.1","type":"urn:x-nmos:control:sr-ctrl/v1.1","authorization":false},
	  {"href":"http://10.6.255.102:3000/x-nmos/node/v1.3/sdp/","type":"urn:x-evs:control:connection","authorization":false}]}`)
	var d Device
	got := roundTripKeys(t, wire, &d)

	for i, c := range got["controls"].([]any) {
		cm := c.(map[string]any)
		if _, ok := cm["authorization"]; !ok {
			t.Errorf("controls[%d] lost authorization on round-trip", i)
		}
	}
	// The vendor-namespaced control URN must survive verbatim — a
	// registry has no business filtering peer control types.
	if got["controls"].([]any)[1].(map[string]any)["type"] != "urn:x-evs:control:connection" {
		t.Error("vendor control type mutated on round-trip")
	}
}

func TestNeuronShapedSenderRoundTrips(t *testing.T) {
	wire := []byte(`{
	 "id":"4903e656-8186-4a9e-b270-ee76704181b4","version":"1787876573:0",
	 "label":"VTX-01","description":"","tags":{},
	 "flow_id":"11111111-2222-4333-8444-555555555555",
	 "transport":"urn:x-nmos:transport:rtp",
	 "device_id":"3a513f69-6543-54f4-b3ca-6625f3377418",
	 "manifest_href":"http://10.6.255.102:3000/x-nmos/node/v1.3/sdp/4903e656-8186-4a9e-b270-ee76704181b4",
	 "interface_bindings":["en8","en12"],
	 "caps":{},
	 "subscription":{"receiver_id":null,"active":true}}`)
	var s Sender
	got := roundTripKeys(t, wire, &s)

	if _, ok := got["caps"]; !ok {
		t.Error("sender lost its registered empty caps on round-trip")
	}
	sub := got["subscription"].(map[string]any)
	if _, ok := sub["receiver_id"]; !ok {
		t.Error("subscription lost the required-null receiver_id key")
	}
	if sub["active"] != true {
		t.Error("subscription.active flipped on round-trip")
	}
}
