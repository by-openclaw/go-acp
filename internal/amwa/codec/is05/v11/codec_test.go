package v11

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

// TestRegisteredAsV11 pins that the minor reaches the registry. A
// codec that compiles but never registers is invisible: the provider
// serves the version trees it finds in is05.SupportedVersions(), so an
// unregistered minor is a /v1.1/ tree that silently does not exist.
func TestRegisteredAsV11(t *testing.T) {
	c, ok := is05.Get("v1.1")
	if !ok {
		t.Fatal("IS-05 v1.1 is not registered")
	}
	if c.SpecID() != is05.SpecID || c.APIVer() != "v1.1" || c.SpecPatch() != "v1.1.2" {
		t.Errorf("identity = %s/%s/%s, want is-05/v1.1/v1.1.2", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	if New() != (Codec{}) {
		t.Error("New must be equivalent to Codec{}")
	}
}

// TestStagedSenderRoundTrip: v1.1 widens transport_params (ST 2022-7
// legs, websocket, mqtt) without changing the staged envelope; a
// two-leg PATCH body survives encode and decode intact.
func TestStagedSenderRoundTrip(t *testing.T) {
	c := Codec{}
	in := is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
		TransportParams: []is05.TransportParams{
			{"destination_ip": "239.1.1.1"},
			{"destination_ip": "239.2.1.1"},
		},
	}
	if err := c.ValidateStagedSender(in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	body, err := c.EncodeStagedSender(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.DecodeStagedSender(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.TransportParams) != 2 || got.TransportParams[1]["destination_ip"] != "239.2.1.1" {
		t.Fatalf("both legs must survive: %+v", got)
	}
}

// TestStagedSenderRefusals: every entry point applies the canonical
// rules; nothing about v1.1 relaxes them.
func TestStagedSenderRefusals(t *testing.T) {
	c := Codec{}
	bad := is05.StagedSender{} // transport_params absent
	if err := c.ValidateStagedSender(bad); err == nil || !strings.Contains(err.Error(), "transport_params") {
		t.Errorf("Validate: got %v", err)
	}
	if _, err := c.EncodeStagedSender(bad); err == nil {
		t.Error("Encode must refuse an invalid staged sender")
	}
	if got, err := c.DecodeStagedSender([]byte(`{"master_enable":`)); err == nil || got.TransportParams != nil {
		t.Errorf("Decode must refuse broken JSON and hand back the zero value, got %+v, %v", got, err)
	}
}

func TestStagedReceiverRoundTrip(t *testing.T) {
	c := Codec{}
	sender := "33333333-3333-4333-8333-333333333333"
	in := is05.StagedReceiver{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		SenderID:          &sender,
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
		TransportParams:   []is05.TransportParams{{"connection_uri": "ws://10.6.250.101:8080/x-nmos/events/v1.0/ws"}},
		TransportFile:     &is05.TransportFile{},
	}
	if err := c.ValidateStagedReceiver(in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	body, err := c.EncodeStagedReceiver(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.DecodeStagedReceiver(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if *got.SenderID != sender || got.TransportParams[0]["connection_uri"] != in.TransportParams[0]["connection_uri"] {
		t.Fatalf("round trip diverged: %+v", got)
	}
}

func TestStagedReceiverRefusals(t *testing.T) {
	c := Codec{}
	bad := is05.StagedReceiver{}
	if err := c.ValidateStagedReceiver(bad); err == nil || !strings.Contains(err.Error(), "transport_params") {
		t.Errorf("Validate: got %v", err)
	}
	if _, err := c.EncodeStagedReceiver(bad); err == nil {
		t.Error("Encode must refuse an invalid staged receiver")
	}
	if got, err := c.DecodeStagedReceiver([]byte(`{"master_enable":`)); err == nil || got.TransportParams != nil {
		t.Errorf("Decode must refuse broken JSON and hand back the zero value, got %+v, %v", got, err)
	}
}
