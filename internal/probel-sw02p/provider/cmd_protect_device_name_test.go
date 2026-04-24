package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestProtectDeviceNameRequestRepliesWithSelfName locks in the
// "controller asks the matrix who it is" path — rx 103 with
// device == server.selfDeviceNumber replies tx 099 with the
// configured self name.
func TestProtectDeviceNameRequestRepliesWithSelfName(t *testing.T) {
	srv := newBareTestServer(t)
	srv.SetSelfDevice(42, "DHS-TEST")

	res, err := srv.dispatch(codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: 42}))
	if err != nil {
		t.Fatalf("dispatch rx 103: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxProtectDeviceNameResponse {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	resp, err := codec.DecodeProtectDeviceNameResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 099: %v", err)
	}
	if resp.Device != 42 || resp.Name != "DHS-TEST" {
		t.Errorf("tx 099 = %+v; want {Device:42 Name:DHS-TEST}", resp)
	}
}

// TestProtectDeviceNameRequestRepliesWithDefaults confirms the
// un-configured server reports the DefaultSelfDevice* constants.
func TestProtectDeviceNameRequestRepliesWithDefaults(t *testing.T) {
	srv := newBareTestServer(t)

	res, err := srv.dispatch(codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: DefaultSelfDeviceNumber}))
	if err != nil {
		t.Fatalf("dispatch rx 103: %v", err)
	}
	resp, _ := codec.DecodeProtectDeviceNameResponse(*res.reply)
	if resp.Device != DefaultSelfDeviceNumber || resp.Name != DefaultSelfDeviceName {
		t.Errorf("tx 099 = %+v; want default self device", resp)
	}
}

// TestProtectDeviceNameRequestUnknownDeviceReturnsEmpty confirms an
// rx 103 for an unknown (non-self, not an owner) Device number
// returns tx 099 with an empty name — §3.2.63 has no dedicated
// "unknown" sentinel so we emit a well-formed but empty reply.
func TestProtectDeviceNameRequestUnknownDeviceReturnsEmpty(t *testing.T) {
	srv := newBareTestServer(t)
	srv.SetSelfDevice(7, "DHS")

	res, err := srv.dispatch(codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: 999}))
	if err != nil {
		t.Fatalf("dispatch rx 103: %v", err)
	}
	resp, _ := codec.DecodeProtectDeviceNameResponse(*res.reply)
	if resp.Device != 999 || resp.Name != "" {
		t.Errorf("tx 099 = %+v; want empty name for unknown device", resp)
	}
}

// TestProtectDeviceNameRequestHandlerDecodeError verifies guards.
func TestProtectDeviceNameRequestHandlerDecodeError(t *testing.T) {
	srv := newBareTestServer(t)
	bad := codec.Frame{ID: codec.RxProtectDeviceNameRequest, Payload: []byte{0x00}}
	_, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
}
