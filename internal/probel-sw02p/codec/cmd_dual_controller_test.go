package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestEncodeDualControllerStatusRequestWireLayout locks rx 050 — the
// zero-length MESSAGE shape (SOM + CMD + checksum = 3 bytes total).
func TestEncodeDualControllerStatusRequestWireLayout(t *testing.T) {
	f := EncodeDualControllerStatusRequest(DualControllerStatusRequestParams{})
	wire := Pack(f)
	// checksum7({32}) = (-(0x32)) & 0x7F = 0x4E.
	want := []byte{SOM, 0x32, 0x4E}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, n, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if n != len(wire) {
		t.Errorf("consumed = %d; want %d", n, len(wire))
	}
	if _, err := DecodeDualControllerStatusRequest(parsed); err != nil {
		t.Errorf("Decode: %v", err)
	}
}

// TestDecodeDualControllerStatusRequestRejects verifies the
// per-command decoder still guards against a wrong CMD.
func TestDecodeDualControllerStatusRequestRejects(t *testing.T) {
	if _, err := DecodeDualControllerStatusRequest(Frame{ID: TxDualControllerStatusResponse}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
}

// TestEncodeDualControllerStatusResponseWireLayout locks tx 051 —
// the 2-byte status MESSAGE.
func TestEncodeDualControllerStatusResponseWireLayout(t *testing.T) {
	cases := []struct {
		name   string
		params DualControllerStatusResponseParams
		want   []byte
	}{
		{
			"master active, idle OK",
			DualControllerStatusResponseParams{Active: ActiveControllerMaster, IdleStatus: IdleControllerOK},
			// checksum7({33, 00, 00}) = (-(0x33)) & 0x7F = 0x4D.
			[]byte{SOM, 0x33, 0x00, 0x00, 0x4D},
		},
		{
			"slave active, idle faulty",
			DualControllerStatusResponseParams{Active: ActiveControllerSlave, IdleStatus: IdleControllerFaulty},
			// checksum7({33, 01, 01}) = (-(0x35)) & 0x7F = 0x4B.
			[]byte{SOM, 0x33, 0x01, 0x01, 0x4B},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeDualControllerStatusResponse(tc.params))
			if !bytes.Equal(wire, tc.want) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.want)
			}
			parsed, _, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			got, err := DecodeDualControllerStatusResponse(parsed)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeDualControllerStatusResponseRejects verifies guards.
func TestDecodeDualControllerStatusResponseRejects(t *testing.T) {
	if _, err := DecodeDualControllerStatusResponse(Frame{ID: RxDualControllerStatusRequest, Payload: []byte{0, 0}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeDualControllerStatusResponse(Frame{ID: TxDualControllerStatusResponse, Payload: []byte{0}}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestUnpackHandlesZeroLenMessage verifies the stream scanner peels
// a 3-byte rx 050 frame correctly — this is the first command in
// this codec with a zero-length MESSAGE, so it stresses the SOM +
// CMD + CHK minimum path.
func TestUnpackHandlesZeroLenMessage(t *testing.T) {
	wire := Pack(EncodeDualControllerStatusRequest(DualControllerStatusRequestParams{}))
	if len(wire) != 3 {
		t.Fatalf("wire len = %d; want 3", len(wire))
	}
	// 2 bytes buffered → not enough yet.
	_, _, err := Unpack(wire[:2])
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("2 bytes: got %v; want io.ErrUnexpectedEOF", err)
	}
	// Full 3 bytes → clean decode.
	f, n, err := Unpack(wire)
	if err != nil || n != 3 {
		t.Errorf("full: n=%d err=%v; want n=3 err=nil", n, err)
	}
	if f.ID != RxDualControllerStatusRequest {
		t.Errorf("parsed ID = %#x; want RxDualControllerStatusRequest", f.ID)
	}
	if len(f.Payload) != 0 {
		t.Errorf("payload len = %d; want 0", len(f.Payload))
	}
}
