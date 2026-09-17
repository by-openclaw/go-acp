package consumer

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

func legsOf(t *testing.T, patch map[string]any) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(patch["transport_params"])
	if err != nil {
		t.Fatalf("marshal transport_params: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("transport_params is not an array of objects: %v", err)
	}
	return out
}

// TestPatchIsAlwaysFullLength is the one that matters for ST 2022-7.
//
// IS-05 matches transport_params POSITIONALLY. Sending a one-element
// array to a two-leg sender does not mean "leave leg 1 alone" — it
// describes leg 0, and on a device where the caller meant leg 1 that
// silently re-addresses the wrong network.
func TestPatchIsAlwaysFullLength(t *testing.T) {
	patch, err := buildSenderPatch(2, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID:       "s",
		DestinationIPs: []string{"", "239.101.40.51"},
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	legs := legsOf(t, patch)
	if len(legs) != 2 {
		t.Fatalf("transport_params has %d entries, want 2 — IS-05 matches legs by position", len(legs))
	}
	if len(legs[0]) != 0 {
		t.Errorf("leg 0 was not asked to change; it must merge to a no-op, got %v", legs[0])
	}
	if legs[1]["destination_ip"] != "239.101.40.51" {
		t.Errorf("leg 1 destination_ip = %v", legs[1]["destination_ip"])
	}
}

func TestPatchBothLegs(t *testing.T) {
	patch, err := buildSenderPatch(2, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID:         "s",
		DestinationIPs:   []string{"239.100.40.51", "239.101.40.51"},
		DestinationPorts: []int{12700, 12700},
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	legs := legsOf(t, patch)
	for i, want := range []string{"239.100.40.51", "239.101.40.51"} {
		if legs[i]["destination_ip"] != want {
			t.Errorf("leg %d destination_ip = %v, want %s", i, legs[i]["destination_ip"], want)
		}
		if legs[i]["destination_port"] != float64(12700) {
			t.Errorf("leg %d destination_port = %v", i, legs[i]["destination_port"])
		}
	}
}

// TestPatchOmitsUnsetKeys: IS-05 PATCH is a MERGE. A key we did not set
// must not appear, or the device takes our zero value as an instruction.
func TestPatchOmitsUnsetKeys(t *testing.T) {
	patch, err := buildSenderPatch(1, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID:       "s",
		DestinationIPs: []string{"239.1.1.1"},
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	if _, present := patch["master_enable"]; present {
		t.Error("master_enable was not requested; sending it would overwrite the device's own")
	}
	leg := legsOf(t, patch)[0]
	if _, present := leg["destination_port"]; present {
		t.Error("destination_port was not requested; sending it would overwrite the device's own")
	}
}

func TestPatchMasterEnableIsExplicit(t *testing.T) {
	for _, want := range []bool{true, false} {
		v := want
		patch, err := buildSenderPatch(1, is05.ActivationModeImmediate, SetSenderRequest{
			SenderID: "s", DestinationIPs: []string{"239.1.1.1"}, MasterEnable: &v,
		})
		if err != nil {
			t.Fatalf("buildSenderPatch: %v", err)
		}
		if patch["master_enable"] != want {
			t.Errorf("master_enable = %v, want %v", patch["master_enable"], want)
		}
	}
}

func TestPatchLegCountMismatch(t *testing.T) {
	cases := []struct {
		name string
		req  SetSenderRequest
		want string
	}{
		{"one destination for two legs",
			SetSenderRequest{SenderID: "s", DestinationIPs: []string{"239.1.1.1"}},
			"2 transport leg(s), you gave 1"},
		{"three destinations for two legs",
			SetSenderRequest{SenderID: "s", DestinationIPs: []string{"a", "b", "c"}},
			"2 transport leg(s), you gave 3"},
		{"one port for two legs",
			SetSenderRequest{SenderID: "s", DestinationPorts: []int{5004}},
			"2 transport leg(s), you gave 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildSenderPatch(2, is05.ActivationModeImmediate, tc.req)
			if err == nil {
				t.Fatal("a leg-count mismatch must be refused, not silently truncated")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error must say how many legs the device has and how many "+
					"values were given, got: %v", err)
			}
		})
	}
}

func TestPatchNoLegs(t *testing.T) {
	if _, err := buildSenderPatch(0, is05.ActivationModeImmediate,
		SetSenderRequest{SenderID: "s", DestinationIPs: []string{"239.1.1.1"}}); err == nil {
		t.Fatal("a sender reporting no transport legs must be an error, not an empty patch")
	}
}

// TestPatchScheduledCarriesRequestedTime: requested_time belongs only
// to the scheduled modes; sending it with activate_immediate is a
// schema violation.
func TestPatchScheduledCarriesRequestedTime(t *testing.T) {
	patch, err := buildSenderPatch(1, is05.ActivationModeScheduledAbsolute, SetSenderRequest{
		SenderID: "s", DestinationIPs: []string{"239.1.1.1"}, When: "1800000037:0",
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	act := patch["activation"].(map[string]any)
	if act["requested_time"] != "1800000037:0" {
		t.Errorf("requested_time = %v", act["requested_time"])
	}

	patch, err = buildSenderPatch(1, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID: "s", DestinationIPs: []string{"239.1.1.1"}, When: "1800000037:0",
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	act = patch["activation"].(map[string]any)
	if _, present := act["requested_time"]; present {
		t.Error("activate_immediate must not carry requested_time")
	}
}

// TestFlattenLegsReadsUntypedParams: transport_params are untyped JSON,
// so numbers arrive as float64 and a missing key must read as empty
// rather than panicking.
func TestFlattenLegsReadsUntypedParams(t *testing.T) {
	got := flattenLegs([]is05.TransportParams{
		{"source_ip": "10.6.40.51", "destination_ip": "0.0.0.0",
			"destination_port": float64(12700), "rtp_enabled": true},
		{}, // a leg the device described with nothing at all
	})
	if len(got) != 2 {
		t.Fatalf("got %d legs", len(got))
	}
	if got[0].SourceIP != "10.6.40.51" || got[0].DestinationIP != "0.0.0.0" {
		t.Errorf("leg 0 = %+v", got[0])
	}
	if got[0].DestinationPort != 12700 || !got[0].RTPEnabled {
		t.Errorf("leg 0 = %+v", got[0])
	}
	if got[1] != (LegState{}) {
		t.Errorf("an empty leg must flatten to zero values, got %+v", got[1])
	}
}
