package codec

import "testing"

// TestKeepaliveRequestRoundtrip: encode+pack+unpack of tx 0x11.
func TestKeepaliveRequestRoundtrip(t *testing.T) {
	f := EncodeKeepaliveRequest()
	if f.ID != TxAppKeepaliveRequest {
		t.Errorf("ID = %#x; want %#x", f.ID, TxAppKeepaliveRequest)
	}
	if len(f.Payload) != 0 {
		t.Errorf("Payload len = %d; want 0", len(f.Payload))
	}

	raw := Pack(f)
	back, consumed, err := Unpack(raw)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if consumed != len(raw) {
		t.Errorf("consumed = %d; want %d", consumed, len(raw))
	}
	if err := DecodeKeepaliveRequest(back); err != nil {
		t.Errorf("DecodeKeepaliveRequest: %v", err)
	}
}

// TestKeepaliveResponseRoundtrip: encode+pack+unpack of rx 0x22.
func TestKeepaliveResponseRoundtrip(t *testing.T) {
	f := EncodeKeepaliveResponse()
	if f.ID != RxAppKeepaliveResponse {
		t.Errorf("ID = %#x; want %#x", f.ID, RxAppKeepaliveResponse)
	}
	if len(f.Payload) != 0 {
		t.Errorf("Payload len = %d; want 0", len(f.Payload))
	}

	raw := Pack(f)
	back, _, err := Unpack(raw)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if err := DecodeKeepaliveResponse(back); err != nil {
		t.Errorf("DecodeKeepaliveResponse: %v", err)
	}
}

// TestKeepaliveWireBytes: exact byte-match (SOM ID BTC CHK EOM).
// DATA = [0x11], BTC = 1, CHK = (-(0x11 + 0x01)) & 0xFF = 0xEE.
func TestKeepaliveWireBytes(t *testing.T) {
	want := []byte{0x10, 0x02, 0x11, 0x01, 0xEE, 0x10, 0x03}
	got := Pack(EncodeKeepaliveRequest())
	if len(got) != len(want) {
		t.Fatalf("len = %d; want %d\ngot:  %s\nwant: %s",
			len(got), len(want), HexDump(got), HexDump(want))
	}
	for i, b := range want {
		if got[i] != b {
			t.Fatalf("byte %d = %#x; want %#x\ngot:  %s\nwant: %s",
				i, got[i], b, HexDump(got), HexDump(want))
		}
	}
}

// TestDecodeKeepaliveRequestRejectsNonEmpty: payload bytes are illegal.
func TestDecodeKeepaliveRequestRejectsNonEmpty(t *testing.T) {
	f := Frame{ID: TxAppKeepaliveRequest, Payload: []byte{0x00}}
	if err := DecodeKeepaliveRequest(f); err == nil {
		t.Error("DecodeKeepaliveRequest: want error on non-empty payload")
	}
}

// TestDecodeKeepaliveRequestRejectsWrongID: wrong CMD id is an error.
func TestDecodeKeepaliveRequestRejectsWrongID(t *testing.T) {
	f := Frame{ID: 0x22}
	if err := DecodeKeepaliveRequest(f); err == nil {
		t.Error("DecodeKeepaliveRequest: want error on wrong ID")
	}
}
