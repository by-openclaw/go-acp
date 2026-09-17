package is05

import (
	"strings"
	"testing"
	"time"
)

func TestActivationModeValidity(t *testing.T) {
	cases := map[ActivationMode]bool{
		ActivationModeNone:                 true,
		ActivationModeImmediate:            true,
		ActivationModeScheduledRelative:    true,
		ActivationModeScheduledAbsolute:    true,
		ActivationMode("activate_unknown"): false,
		ActivationMode("garbage"):          false,
	}
	for m, want := range cases {
		if got := IsValidActivationMode(m); got != want {
			t.Errorf("IsValidActivationMode(%q) = %v, want %v", m, got, want)
		}
	}
}

func TestValidateActivationModeRules(t *testing.T) {
	now := FormatTAINow(time.Now())
	cases := []struct {
		name string
		a    Activation
		ok   bool
	}{
		{"none-empty", Activation{}, true},
		{"none-with-time", Activation{RequestedTime: &now}, false},
		{"immediate-clean", Activation{Mode: ActivationModeImmediate}, true},
		{"immediate-with-time", Activation{Mode: ActivationModeImmediate, RequestedTime: &now}, false},
		{"scheduled-rel-time", Activation{Mode: ActivationModeScheduledRelative, RequestedTime: &now}, true},
		{"scheduled-rel-no-time", Activation{Mode: ActivationModeScheduledRelative}, false},
		{"scheduled-abs-bad-tai", Activation{Mode: ActivationModeScheduledAbsolute, RequestedTime: ptrStr("not-tai")}, false},
		{"unknown-mode", Activation{Mode: "garbage"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateActivation(tc.a)
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestStagedSenderRoundTrip(t *testing.T) {
	s := StagedSender{
		MasterEnableField: MasterEnableField{MasterEnable: true},
		Activation:        Activation{Mode: ActivationModeImmediate},
		TransportParams:   []TransportParams{{}},
	}
	body, err := EncodeStagedSender(s)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeStagedSender(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Activation.Mode != ActivationModeImmediate {
		t.Fatalf("round-trip mismatch")
	}
}

func TestStagedSenderDecodeRejectsUnknownField(t *testing.T) {
	body := []byte(`{"master_enable":true,"receiver_id":null,"activation":{"mode":"","requested_time":null,"activation_time":null},"transport_params":[],"future_field":"oops"}`)
	if _, err := DecodeStagedSender(body); err == nil {
		t.Fatalf("expected DisallowUnknownFields rejection")
	} else if !strings.Contains(err.Error(), "future_field") {
		t.Fatalf("error should name unknown field, got %v", err)
	}
}

func TestStagedReceiverValidateRequiresTransportParams(t *testing.T) {
	r := StagedReceiver{} // TransportParams nil
	if err := ValidateStagedReceiver(r); err == nil {
		t.Fatalf("expected error for nil transport_params")
	}
}

func ptrStr(s string) *string { return &s }
