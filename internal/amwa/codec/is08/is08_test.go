package is08

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestActivationModeValidity(t *testing.T) {
	for _, m := range []ActivationMode{
		ActivationModeImmediate,
		ActivationModeScheduledRelative,
		ActivationModeScheduledAbsolute,
	} {
		if !IsValidActivationMode(m) {
			t.Errorf("expected %q valid", m)
		}
	}
	for _, m := range []ActivationMode{"", "garbage", "activate_unknown"} {
		if IsValidActivationMode(m) {
			t.Errorf("expected %q invalid", m)
		}
	}
}

func TestValidateActivationRules(t *testing.T) {
	cases := []struct {
		name string
		a    Activation
		ok   bool
	}{
		{"immediate-clean", Activation{Mode: ActivationModeImmediate}, true},
		{"immediate-with-time", Activation{Mode: ActivationModeImmediate, RequestedTime: ptr("1:0")}, false},
		{"scheduled-rel-time", Activation{Mode: ActivationModeScheduledRelative, RequestedTime: ptr("1:0")}, true},
		{"scheduled-abs-time", Activation{Mode: ActivationModeScheduledAbsolute, RequestedTime: ptr("1:0")}, true},
		{"scheduled-no-time", Activation{Mode: ActivationModeScheduledRelative}, false},
		{"scheduled-bad-tai", Activation{Mode: ActivationModeScheduledRelative, RequestedTime: ptr("not-tai")}, false},
		{"empty-mode", Activation{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateActivation(tc.a)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("want error")
			}
		})
	}
}

func TestValidateMapEntries(t *testing.T) {
	cases := []struct {
		name string
		m    MapEntries
		ok   bool
	}{
		{"empty", MapEntries{}, true},
		{"valid-routed", MapEntries{
			"out1": {"0": MapEntry{Input: ptr("in1"), ChannelIndex: ptr(0)}},
		}, true},
		{"valid-unrouted", MapEntries{
			"out1": {"0": MapEntry{}},
		}, true},
		{"bad-output-id", MapEntries{
			"out 1": {"0": MapEntry{}},
		}, false},
		{"bad-channel-key", MapEntries{
			"out1": {"01": MapEntry{}},
		}, false},
		{"bad-channel-key-negative", MapEntries{
			"out1": {"-1": MapEntry{}},
		}, false},
		{"input-set-but-no-channel", MapEntries{
			"out1": {"0": MapEntry{Input: ptr("in1")}},
		}, false},
		{"channel-set-but-no-input", MapEntries{
			"out1": {"0": MapEntry{ChannelIndex: ptr(0)}},
		}, false},
		{"input-bad-id", MapEntries{
			"out1": {"0": MapEntry{Input: ptr("bad id"), ChannelIndex: ptr(0)}},
		}, false},
		{"channel-negative", MapEntries{
			"out1": {"0": MapEntry{Input: ptr("in1"), ChannelIndex: ptr(-1)}},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMapEntries(tc.m)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("want error")
			}
		})
	}
}

func TestMapActiveRoundTrip(t *testing.T) {
	in := MapActive{
		Activation: ActivationResponse{
			Mode:           ptrMode(ActivationModeImmediate),
			RequestedTime:  nil,
			ActivationTime: ptr("100:200"),
		},
		Map: MapEntries{
			"out1": {
				"0": MapEntry{Input: ptr("in1"), ChannelIndex: ptr(0)},
				"1": MapEntry{}, // unrouted
			},
		},
	}
	body, err := EncodeMapActive(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeMapActive(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Map["out1"]["0"].Input == nil || *got.Map["out1"]["0"].Input != "in1" {
		t.Fatalf("routed input mismatch: %+v", got.Map["out1"]["0"])
	}
	if got.Map["out1"]["1"].Input != nil {
		t.Fatalf("unrouted entry should have nil input")
	}
}

func TestMapActiveDecodeRejectsUnknownField(t *testing.T) {
	body := []byte(`{"activation":{"mode":null,"requested_time":null,"activation_time":null},"map":{},"future_field":"x"}`)
	if _, err := DecodeMapActive(body); err == nil {
		t.Fatalf("expected DisallowUnknownFields rejection")
	} else if !strings.Contains(err.Error(), "future_field") {
		t.Fatalf("error should name unknown field: %v", err)
	}
}

func TestMapActivationRequestRoundTrip(t *testing.T) {
	in := MapActivationRequest{
		Activation: Activation{
			Mode:          ActivationModeScheduledAbsolute,
			RequestedTime: ptr("1700000000:0"),
		},
		Action: MapEntries{
			"out1": {"0": MapEntry{Input: ptr("in1"), ChannelIndex: ptr(0)}},
		},
	}
	body, err := EncodeMapActivationRequest(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeMapActivationRequest(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Activation.Mode != ActivationModeScheduledAbsolute {
		t.Fatalf("mode mismatch")
	}
	if *got.Activation.RequestedTime != "1700000000:0" {
		t.Fatalf("requested_time mismatch")
	}
}

func TestIORoundTrip(t *testing.T) {
	in := IO{
		Inputs: map[string]Input{
			"in1": {
				Properties: &InputProperties{Name: "Input 1", Description: "Mic 1"},
				Caps:       &InputCaps{Reordering: true, BlockSize: 1},
				Channels:   []Channel{{Label: "L"}, {Label: "R"}},
				Parent: &InputParent{
					ID:   ptr("1f1e1d1c-1b1a-4019-8817-161514131211"),
					Type: ptr("source"),
				},
			},
		},
		Outputs: map[string]Output{
			"out1": {
				Properties: &OutputProperties{Name: "Output 1", Description: "Bus A"},
				Caps:       &OutputCaps{RoutableInputs: []*string{ptr("in1"), nil}},
				Channels:   []Channel{{Label: "L"}, {Label: "R"}},
				SourceID:   ptr("2f2e2d2c-2b2a-4029-8827-262524232221"),
			},
		},
	}
	body, err := EncodeIO(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeIO(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Inputs["in1"].Caps.BlockSize != 1 {
		t.Fatalf("input caps mismatch")
	}
	if got.Outputs["out1"].Channels[1].Label != "R" {
		t.Fatalf("output channel mismatch")
	}
}

func TestIORejectsBadOutputID(t *testing.T) {
	in := IO{
		Inputs:  map[string]Input{},
		Outputs: map[string]Output{"out 1": {}},
	}
	if _, err := EncodeIO(in); err == nil {
		t.Fatalf("expected error on bad id")
	}
}

func TestIORequiresInputsOutputs(t *testing.T) {
	if err := ValidateIO(IO{}); err == nil {
		t.Fatalf("expected required inputs/outputs")
	}
}

func TestInputCapsBlockSize(t *testing.T) {
	if err := ValidateInputCaps(InputCaps{BlockSize: 0}); err == nil {
		t.Fatalf("block_size 0 should fail")
	}
	if err := ValidateInputCaps(InputCaps{BlockSize: 1}); err != nil {
		t.Fatalf("block_size 1 should pass: %v", err)
	}
}

func TestOutputCapsRoutableInputs(t *testing.T) {
	if err := ValidateOutputCaps(OutputCaps{RoutableInputs: nil}); err != nil {
		t.Fatalf("nil routable_inputs should pass: %v", err)
	}
	if err := ValidateOutputCaps(OutputCaps{RoutableInputs: []*string{ptr("in 1")}}); err == nil {
		t.Fatalf("bad id should fail")
	}
	if err := ValidateOutputCaps(OutputCaps{RoutableInputs: []*string{nil, ptr("in1")}}); err != nil {
		t.Fatalf("explicit-null + valid id should pass: %v", err)
	}
}

func TestInputParent(t *testing.T) {
	if err := ValidateInputParent(InputParent{ID: ptr("garbage"), Type: ptr("source")}); err == nil {
		t.Fatalf("bad uuid should fail")
	}
	if err := ValidateInputParent(InputParent{ID: ptr("1f1e1d1c-1b1a-4019-8817-161514131211"), Type: ptr("invalid")}); err == nil {
		t.Fatalf("bad type should fail")
	}
	if err := ValidateInputParent(InputParent{}); err != nil {
		t.Fatalf("all-null should pass: %v", err)
	}
}

func TestActivationResponseValidity(t *testing.T) {
	mode := ActivationModeImmediate
	if err := ValidateActivationResponse(ActivationResponse{Mode: &mode, ActivationTime: ptr("not-tai")}); err == nil {
		t.Fatalf("bad activation_time should fail")
	}
	if err := ValidateActivationResponse(ActivationResponse{}); err != nil {
		t.Fatalf("all-null should pass: %v", err)
	}
}

func ptrMode(m ActivationMode) *ActivationMode { return &m }
