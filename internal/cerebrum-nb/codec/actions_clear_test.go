package codec

import (
	"strings"
	"testing"
)

// TestEncodeAction_MnemonicClear pins the label-CLEAR wire form: with
// EmptyMnemonic set, MNEMONIC="" is force-emitted (normally empty attrs are
// omitted); without it, an empty Mnemonic stays off the wire. 0v16 does not
// document clearing — this explicit-empty form is the plausible mechanism,
// gated behind the CLI's --allow-clear until live-verified.
func TestEncodeAction_MnemonicClear(t *testing.T) {
	clear := EncodeAction(7, &RoutingAction{
		Type: "SRCE_MNE", IPAddress: "0.0.0.0", DeviceType: DeviceType("ROUTER"),
		SrceID: "11", AltMne: "1", EmptyMnemonic: true,
	})
	if !strings.Contains(string(clear), `MNEMONIC=""`) {
		t.Errorf("clear form must force-emit MNEMONIC=\"\": %s", clear)
	}
	if !strings.Contains(string(clear), `ALT_MNE="1"`) {
		t.Errorf("clear form must keep the alt slot: %s", clear)
	}

	normal := EncodeAction(8, &RoutingAction{
		Type: "SRCE_MNE", IPAddress: "0.0.0.0", DeviceType: DeviceType("ROUTER"),
		SrceID: "11",
	})
	if strings.Contains(string(normal), "MNEMONIC") {
		t.Errorf("without EmptyMnemonic an empty Mnemonic must be omitted: %s", normal)
	}
}
