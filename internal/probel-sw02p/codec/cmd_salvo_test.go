package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestEncodeConnectOnGoByteLayout pins the wire layout of rx 05 against
// SW-P-02 §3.2.7 + §3.2.3. Exercises narrow addressing (dst/src < 128)
// and the extended-addressing path (dst/src in the 128-1023 range) so
// the Multiplier bit-packing is covered on both halves.
func TestEncodeConnectOnGoByteLayout(t *testing.T) {
	cases := []struct {
		name   string
		params ConnectOnGoParams
		// Expected wire = SOM || cmd=0x05 || Multiplier || DestMod128 || SrcMod128 || checksum
		wantWire []byte
	}{
		{
			name:   "dst=1 src=2 narrow",
			params: ConnectOnGoParams{Destination: 1, Source: 2},
			// Multiplier = 0x00, DestMod128 = 0x01, SrcMod128 = 0x02.
			// checksum7({05, 00, 01, 02}) = (-(0x08)) & 0x7F = 0x78.
			wantWire: []byte{SOM, 0x05, 0x00, 0x01, 0x02, 0x78},
		},
		{
			name:   "dst=130 src=260 extended",
			params: ConnectOnGoParams{Destination: 130, Source: 260},
			// DestinationDIV128 = 1 → bit 4-6 = 0b001 → 0x10.
			// SourceDIV128      = 2 → bit 0-2 = 0b010 → 0x02.
			// Multiplier = 0x12.
			// DestMod128 = 130 - 128 = 2.
			// SrcMod128  = 260 - 256 = 4.
			// checksum7({05, 12, 02, 04}) = (-(0x1D)) & 0x7F = 0x63.
			wantWire: []byte{SOM, 0x05, 0x12, 0x02, 0x04, 0x63},
		},
		{
			name:   "bad source flag",
			params: ConnectOnGoParams{Destination: 0, Source: 0, BadSource: true},
			// Multiplier = 0x08 (bit 3). DestMod128=0 SrcMod128=0.
			// checksum7({05, 08, 00, 00}) = (-(0x0D)) & 0x7F = 0x73.
			wantWire: []byte{SOM, 0x05, 0x08, 0x00, 0x00, 0x73},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := EncodeConnectOnGo(tc.params)
			wire := Pack(f)
			if !bytes.Equal(wire, tc.wantWire) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.wantWire)
			}

			// Round-trip: parse the wire and verify the decoded
			// ConnectOnGoParams matches the input.
			parsed, n, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			if n != len(wire) {
				t.Errorf("consumed = %d; want %d", n, len(wire))
			}
			got, err := DecodeConnectOnGo(parsed)
			if err != nil {
				t.Fatalf("DecodeConnectOnGo: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeConnectOnGoRejectsWrongID confirms the per-command decoder
// refuses a frame whose ID is not RxConnectOnGo.
func TestDecodeConnectOnGoRejectsWrongID(t *testing.T) {
	f := Frame{ID: TxConnectOnGoAck, Payload: []byte{0x00, 0x01, 0x02}}
	_, err := DecodeConnectOnGo(f)
	if !errors.Is(err, ErrWrongCommand) {
		t.Errorf("got %v; want ErrWrongCommand", err)
	}
}

// TestDecodeConnectOnGoRejectsShortPayload confirms the per-command
// decoder refuses a frame whose MESSAGE is shorter than the 3-byte
// minimum.
func TestDecodeConnectOnGoRejectsShortPayload(t *testing.T) {
	f := Frame{ID: RxConnectOnGo, Payload: []byte{0x00, 0x01}} // 2 bytes
	_, err := DecodeConnectOnGo(f)
	if !errors.Is(err, ErrShortPayload) {
		t.Errorf("got %v; want ErrShortPayload", err)
	}
}

// TestEncodeConnectOnGoAckByteLayout pins the wire layout of tx 12
// against SW-P-02 §3.2.14. §3.2.14 notes: Multiplier bit 3 ("bad
// source") is always 0 on the ack regardless of the request state.
func TestEncodeConnectOnGoAckByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		params   ConnectOnGoAckParams
		wantWire []byte
	}{
		{
			name:   "dst=1 src=2",
			params: ConnectOnGoAckParams{Destination: 1, Source: 2},
			// Multiplier=0x00 DestMod128=0x01 SrcMod128=0x02.
			// checksum7({0C, 00, 01, 02}) = (-(0x0F)) & 0x7F = 0x71.
			wantWire: []byte{SOM, 0x0C, 0x00, 0x01, 0x02, 0x71},
		},
		{
			name:   "dst=130 src=260 extended",
			params: ConnectOnGoAckParams{Destination: 130, Source: 260},
			// Multiplier=0x12 DestMod128=2 SrcMod128=4.
			// checksum7({0C, 12, 02, 04}) = (-(0x24)) & 0x7F = 0x5C.
			wantWire: []byte{SOM, 0x0C, 0x12, 0x02, 0x04, 0x5C},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := EncodeConnectOnGoAck(tc.params)
			wire := Pack(f)
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
			got, err := DecodeConnectOnGoAck(parsed)
			if err != nil {
				t.Fatalf("DecodeConnectOnGoAck: %v", err)
			}
			if got != tc.params {
				t.Errorf("decoded = %+v; want %+v", got, tc.params)
			}
		})
	}
}

// TestDecodeConnectOnGoAckRejectsWrongID confirms the per-command
// decoder refuses a frame whose ID is not TxConnectOnGoAck.
func TestDecodeConnectOnGoAckRejectsWrongID(t *testing.T) {
	f := Frame{ID: RxConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}}
	_, err := DecodeConnectOnGoAck(f)
	if !errors.Is(err, ErrWrongCommand) {
		t.Errorf("got %v; want ErrWrongCommand", err)
	}
}

// TestEncodeConnectedByteLayout pins the wire layout of tx 04
// CONNECTED against SW-P-02 §3.2.6 + §3.2.3. Same Multiplier layout
// as rx 05 / tx 12 — cross-check with those tests.
func TestEncodeConnectedByteLayout(t *testing.T) {
	f := EncodeConnected(ConnectedParams{Destination: 1, Source: 2})
	wire := Pack(f)
	// Multiplier=0x00 Dst=0x01 Src=0x02.
	// checksum7({04, 00, 01, 02}) = (-0x07) & 0x7F = 0x79.
	want := []byte{SOM, 0x04, 0x00, 0x01, 0x02, 0x79}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeConnected(parsed)
	if err != nil {
		t.Fatalf("DecodeConnected: %v", err)
	}
	if got.Destination != 1 || got.Source != 2 {
		t.Errorf("decoded = %+v; want dst=1 src=2", got)
	}
}

// TestEncodeGoByteLayout pins the wire layout of rx 06 GO against
// §3.2.8. One-byte MESSAGE: 00 = set, 01 = clear.
func TestEncodeGoByteLayout(t *testing.T) {
	cases := []struct {
		name     string
		op       GoOperation
		wantWire []byte
	}{
		{
			name: "set",
			op:   GoOpSet,
			// checksum7({06, 00}) = (-0x06) & 0x7F = 0x7A.
			wantWire: []byte{SOM, 0x06, 0x00, 0x7A},
		},
		{
			name: "clear",
			op:   GoOpClear,
			// checksum7({06, 01}) = (-0x07) & 0x7F = 0x79.
			wantWire: []byte{SOM, 0x06, 0x01, 0x79},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := EncodeGo(GoParams{Operation: tc.op})
			wire := Pack(f)
			if !bytes.Equal(wire, tc.wantWire) {
				t.Fatalf("wire\n got %X\nwant %X", wire, tc.wantWire)
			}
			parsed, _, err := Unpack(wire)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			got, err := DecodeGo(parsed)
			if err != nil {
				t.Fatalf("DecodeGo: %v", err)
			}
			if got.Operation != tc.op {
				t.Errorf("op = %#x; want %#x", got.Operation, tc.op)
			}
		})
	}
}

// TestEncodeGoDoneAckByteLayout pins the wire layout of tx 13 GO DONE
// ACKNOWLEDGE against §3.2.15. One-byte MESSAGE mirrors rx 06 op.
func TestEncodeGoDoneAckByteLayout(t *testing.T) {
	f := EncodeGoDoneAck(GoDoneAckParams{Operation: GoOpSet})
	wire := Pack(f)
	// checksum7({0D, 00}) = (-0x0D) & 0x7F = 0x73.
	want := []byte{SOM, 0x0D, 0x00, 0x73}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, _, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	got, err := DecodeGoDoneAck(parsed)
	if err != nil {
		t.Fatalf("DecodeGoDoneAck: %v", err)
	}
	if got.Operation != GoOpSet {
		t.Errorf("op = %#x; want GoOpSet", got.Operation)
	}
}
