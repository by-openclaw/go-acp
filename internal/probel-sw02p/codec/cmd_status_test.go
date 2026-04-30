package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeStatusRequestByteLayout pins the wire layout of rx 07
// against SW-P-02 §3.2.9. Covers LH (default, single-controller) and
// RH controller selection.
func TestEncodeStatusRequestByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   StatusRequestParams
		wantWire []byte
	}{
		{
			name:   "LH controller (default)",
			params: StatusRequestParams{Controller: ControllerLH},
			// checksum7({07, 00}) = (-(0x07)) & 0x7F = 0x79.
			wantWire: []byte{SOM, 0x07, 0x00, 0x79},
		},
		{
			name:   "RH controller",
			params: StatusRequestParams{Controller: ControllerRH},
			// checksum7({07, 01}) = (-(0x08)) & 0x7F = 0x78.
			wantWire: []byte{SOM, 0x07, 0x01, 0x78},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeStatusRequest(tc.params))
			if !bytes.Equal(wire, tc.wantWire) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.wantWire)
			}
			parsed, n, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			if n != len(wire) {
				t.Errorf("consumed = %d; want %d", n, len(wire))
			}
			got, err := DecodeStatusRequest(parsed)
			if err != nil {
				t.Fatalf("DecodeStatusRequest: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeStatusRequestRejects verifies per-command decoder guards.
func TestDecodeStatusRequestRejects(t *testing.T) {
	if _, err := DecodeStatusRequest(Frame{ID: TxStatusResponse2, Payload: []byte{0}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeStatusRequest(Frame{ID: RxStatusRequest, Payload: nil}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}

// TestEncodeStatusResponse2ByteLayout pins the wire layout of tx 09
// against SW-P-02 §3.2.11. Exercises every fault bit and the
// healthy-default case.
func TestEncodeStatusResponse2ByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   StatusResponse2Params
		wantWire []byte
	}{
		{
			name:   "healthy default",
			params: StatusResponse2Params{},
			// Status byte = 0x00. checksum7({09, 00}) = (-(0x09)) & 0x7F = 0x77.
			wantWire: []byte{SOM, 0x09, 0x00, 0x77},
		},
		{
			name:   "idle flag only",
			params: StatusResponse2Params{Idle: true},
			// bit 6 → 0x40. checksum7({09, 40}) = (-(0x49)) & 0x7F = 0x37.
			wantWire: []byte{SOM, 0x09, 0x40, 0x37},
		},
		{
			name:   "bus fault only",
			params: StatusResponse2Params{BusFault: true},
			// bit 5 → 0x20. checksum7({09, 20}) = (-(0x29)) & 0x7F = 0x57.
			wantWire: []byte{SOM, 0x09, 0x20, 0x57},
		},
		{
			name:   "overheat only",
			params: StatusResponse2Params{Overheat: true},
			// bit 4 → 0x10. checksum7({09, 10}) = (-(0x19)) & 0x7F = 0x67.
			wantWire: []byte{SOM, 0x09, 0x10, 0x67},
		},
		{
			name:   "all fault bits",
			params: StatusResponse2Params{Idle: true, BusFault: true, Overheat: true},
			// bits 4|5|6 → 0x70. checksum7({09, 70}) = (-(0x79)) & 0x7F = 0x07.
			wantWire: []byte{SOM, 0x09, 0x70, 0x07},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := Pack(EncodeStatusResponse2(tc.params))
			if !bytes.Equal(wire, tc.wantWire) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.wantWire)
			}
			parsed, n, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			if n != len(wire) {
				t.Errorf("consumed = %d; want %d", n, len(wire))
			}
			got, err := DecodeStatusResponse2(parsed)
			if err != nil {
				t.Fatalf("DecodeStatusResponse2: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeStatusResponse2Rejects verifies per-command decoder guards.
func TestDecodeStatusResponse2Rejects(t *testing.T) {
	if _, err := DecodeStatusResponse2(Frame{ID: RxStatusRequest, Payload: []byte{0}}); !errors.Is(err, ErrWrongCommand) {
		t.Errorf("wrong ID: got %v; want ErrWrongCommand", err)
	}
	if _, err := DecodeStatusResponse2(Frame{ID: TxStatusResponse2, Payload: nil}); !errors.Is(err, ErrShortPayload) {
		t.Errorf("short payload: got %v; want ErrShortPayload", err)
	}
}
