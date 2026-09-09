package v12

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

func TestNewIsTheZeroCodec(t *testing.T) {
	if New() != (Codec{}) {
		t.Fatal("New must be equivalent to Codec{}")
	}
}

// TestStagedSenderRoundTrip: v1.2 changes permission (which transports
// may appear), not shape; the staged envelope is the canonical one and
// a PATCH body survives encode and decode intact.
func TestStagedSenderRoundTrip(t *testing.T) {
	c := Codec{}
	in := is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
		TransportParams:   []is05.TransportParams{{"source_url": "ndi://dhs.local/cam1"}},
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
	if !got.MasterEnable || got.TransportParams[0]["source_url"] != "ndi://dhs.local/cam1" {
		t.Fatalf("round trip diverged: %+v", got)
	}
}

// TestStagedSenderRefusals: every entry point applies the canonical
// rules; nothing about v1.2 relaxes them.
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
		TransportParams:   []is05.TransportParams{{"source_url": "ndi://dhs.local/cam1"}},
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
	if *got.SenderID != sender || got.TransportParams[0]["source_url"] != "ndi://dhs.local/cam1" {
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
